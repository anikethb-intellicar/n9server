
"""
N9 Dashcam Server — main.py
============================

Three TCP listeners + one HTTP server, all async.

Ports:
  JT808_PORT  (default 6608) — JT808 device signalling
  JT1078_PORT (default 1078) — JT1078 raw video/audio stream
  HTTP_PORT   (default 8080) — HLS output + web player + status API

New features vs original (ported from Go server):
  ✅ Dynamic auth codes        — unique per registration, validated on AUTH
  ✅ Auth-state enforcement    — heartbeat/location rejected if not authed
  ✅ Batch location (0x0704)   — parses and logs all cached GPS items
  ✅ Terminal logout (0x0003)  — cleans up state, revokes auth code
  ✅ Read/write timeouts       — 60s read, 10s write (via asyncio.wait_for)
  ✅ Raw hex TX/RX logging     — every frame logged at DEBUG level
"""

import asyncio
import json
import logging
import argparse
import struct
import time
import socket
from pathlib import Path

from hls import StreamManager

from jt808 import (
    parse_frame,
    build_general_resp, build_register_resp, build_av_request, build_av_close,
    build_query_all_params,
    parse_register_info, parse_location, parse_batch_location,
    AuthCodeStore,
    MSG_REGISTER, MSG_AUTH, MSG_HEARTBEAT, MSG_LOCATION,
    MSG_LOCATION_BATCH, MSG_TERMINAL_LOGOUT,
    MSG_MEDIA_INFO, MSG_MEDIA_UPLOAD,
    MSG_REGISTER_RESP, MSG_GENERAL_RESP,
    MSG_QUERY_ALL_PARAMS, MSG_PARAMS_RESP,
    FRAME_MARKER,
    RESULT_SUCCESS, RESULT_FAILURE, RESULT_NOT_SUPPORTED, RESULT_MESSAGE_ERROR,
)
from jt1078 import StreamBuffer, FrameAssembler
from hls import StreamManager

logging.basicConfig(
    level=logging.DEBUG,
    format="%(asctime)s %(levelname)-7s %(name)-10s %(message)s",
    datefmt="%H:%M:%S",
)
log = logging.getLogger("main")

# ── Timeouts (seconds) ───────────────────────────────────────────────────────
READ_TIMEOUT  = 30    # JT1078: re-request faster when camera pauses (was 300s)
WRITE_TIMEOUT = 10    # how long a send is allowed to take

# ── Shared state ─────────────────────────────────────────────────────────────
auth_store  = AuthCodeStore()

# phone -> {phone, registered, authed, online, last_seen, location, peer, reg_info}
devices: dict[str, dict] = {}

# Per-device stream managers: phone -> StreamManager
device_streams: dict[str, StreamManager] = {}

def get_device_streams(phone: str) -> StreamManager:
    if phone not in device_streams:
        device_streams[phone] = StreamManager()
    return device_streams[phone]

# Active authenticated JT808 connections
active_jt808: dict[str, object] = {}

# Legacy alias (used in JT1078Connection)
stream_mgr = None  # replaced by per-device lookup

_NUM_CAMERAS = 4

# Active JT1078 owner per (phone, channel). When a new connection claims a channel it
# evicts the previous one so only one source ever writes to a given LiveStreamer.
_jt1078_owners: dict[tuple, object] = {}


# ─────────────────────────────────────────────────────────────────────────────
#  JT808  —  signalling connection
# ─────────────────────────────────────────────────────────────────────────────

class JT808Connection:
    def __init__(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter,
                 server_ip: str, jt1078_port: int):
        self.reader      = reader
        self.writer      = writer
        self.server_ip   = server_ip
        self.jt1078_port = jt1078_port
        self.phone: str | None = None
        self._serial     = 0
        self._buf        = bytearray()
        self._is_2019    = False   # set on first message from device
        self._heartbeat_count = 0
        addr             = writer.get_extra_info("peername")
        self.peer        = f"{addr[0]}:{addr[1]}" if addr else "?"

    # ── Helpers ──────────────────────────────────────────────────────────────

    def _next_serial(self) -> int:
        self._serial = (self._serial + 1) & 0xFFFF
        return self._serial

    async def _send(self, data: bytes):
        """Write with timeout + raw hex TX logging."""
        log.debug("TX %d bytes to %s | hex: %s", len(data), self.peer, data.hex())
        try:
            self.writer.write(data)
            await asyncio.wait_for(self.writer.drain(), timeout=WRITE_TIMEOUT)
        except (asyncio.TimeoutError, ConnectionResetError, BrokenPipeError) as e:
            log.warning("TX failed to %s: %s", self.peer, e)

    def _is_authed(self) -> bool:
        return bool(self.phone and devices.get(self.phone, {}).get("authed"))

    # ── Main loop ─────────────────────────────────────────────────────────────

    async def run(self):
        log.info("JT808 connect from %s", self.peer)
        try:
            while True:
                try:
                    chunk = await asyncio.wait_for(self.reader.read(4096), timeout=READ_TIMEOUT)
                except asyncio.TimeoutError:
                    log.warning("Read timeout for %s — closing", self.peer)
                    break

                if not chunk:
                    break

                # Raw hex RX logging
                log.debug("RX %d bytes from %s | hex: %s", len(chunk), self.peer, chunk.hex())

                self._buf += chunk
                await self._process_buf()

        except (asyncio.IncompleteReadError, ConnectionResetError, BrokenPipeError):
            pass
        finally:
            phone = self.phone or self.peer
            log.info("JT808 disconnect: %s", phone)
            if self.phone:
                # Remove device completely on disconnect
                devices.pop(self.phone, None)
                auth_store.revoke(self.phone)
                active_jt808.pop(self.phone, None)
                # Stop and clean up streams for this device
                mgr = device_streams.pop(self.phone, None)
                if mgr:
                    asyncio.ensure_future(mgr.stop_all())

    async def _process_buf(self):
        while True:
            start = self._buf.find(FRAME_MARKER)
            if start == -1:
                self._buf.clear()
                return
            if start > 0:
                self._buf = self._buf[start:]

            end = self._buf.find(FRAME_MARKER, 1)
            if end == -1:
                return

            raw = bytes(self._buf[:end + 1])
            self._buf = self._buf[end + 1:]

            msg = parse_frame(raw)
            if msg:
                await self._handle(msg)

    # ── Message dispatcher ────────────────────────────────────────────────────

    async def _handle(self, msg):
        hdr = msg.header
        mid = hdr.msg_id

        self._is_2019 = hdr.is_2019
        log.debug("MSG 0x%04X from %s serial=%d body=%d bytes",
                  mid, hdr.phone, hdr.serial, len(msg.body))

        if mid == MSG_REGISTER:
            await self._on_register(hdr, msg.body)
        elif mid == MSG_AUTH:
            await self._on_auth(hdr, msg.body)
        elif mid == MSG_HEARTBEAT:
            await self._on_heartbeat(hdr)
        elif mid == MSG_LOCATION:
            await self._on_location(hdr, msg.body)
        elif mid == MSG_LOCATION_BATCH:
            await self._on_location_batch(hdr, msg.body)
        elif mid == MSG_TERMINAL_LOGOUT:
            await self._on_logout(hdr)
        elif mid in (MSG_MEDIA_INFO, MSG_MEDIA_UPLOAD):
            await self._on_media(hdr, mid)
        elif mid == 0x0001:
            # Terminal general response — acknowledgment of our commands, no reply needed
            log.debug("Terminal ACK 0x0001 from %s serial=%d", hdr.phone, hdr.serial)
        elif mid == MSG_PARAMS_RESP:
            self._on_params_resp(msg.body)
        else:
            log.debug("Unsupported msg 0x%04X from %s", mid, hdr.phone)
            resp = build_general_resp(hdr.phone, hdr.serial, mid,
                                      result=RESULT_NOT_SUPPORTED,
                                      serial=self._next_serial(),
                                      is_2019=self._is_2019)
            await self._send(resp)

    # ── Handlers ─────────────────────────────────────────────────────────────

    async def _on_register(self, hdr, body: bytes):
        self.phone = hdr.phone
        reg_info   = parse_register_info(body)

        devices[self.phone] = {
            "phone":      self.phone,
            "registered": True,
            "authed":     False,
            "online":     True,
            "last_seen":  time.time(),
            "location":   {},
            "peer":       self.peer,
            "reg_info":   vars(reg_info) if reg_info else {},
        }

        # ── Dynamic auth code ─────────────────────────────────────────────
        auth_code = auth_store.issue(self.phone)

        if reg_info:
            log.info(
                "REGISTER %s | manufacturer=%s model=%s terminal_id=%s plate=%s province=%04X city=%04X",
                self.phone, reg_info.manufacturer_id, reg_info.terminal_model,
                reg_info.terminal_id, reg_info.license_plate,
                reg_info.province_id, reg_info.city_id,
            )
        else:
            log.info("REGISTER %s (body parse failed)", self.phone)

        log.info("Issuing auth code for %s: %s", self.phone, auth_code)

        resp = build_register_resp(self.phone, hdr.serial,
                                   result=RESULT_SUCCESS,
                                   auth_code=auth_code,
                                   is_2019=self._is_2019)
        await self._send(resp)

    async def _on_auth(self, hdr, body: bytes):
        self.phone = hdr.phone
        # JT808-2019 auth body: [1-byte len] [auth_code] [device_info...]
        # JT808-2013 auth body: just the auth code bytes
        if hdr.is_2019 and len(body) > 0:
            auth_len = body[0]
            presented_code = body[1:1 + auth_len].decode("ascii", errors="replace")
        else:
            presented_code = body.decode("ascii", errors="replace")

        log.info("AUTH %s | code=%r", self.phone, presented_code)

        # ── Auth-state enforcement ────────────────────────────────────────
        if auth_store.validate(self.phone, presented_code):
            result = RESULT_SUCCESS
            if self.phone not in devices:
                devices[self.phone] = {
                    "phone": self.phone, "registered": True, "authed": False,
                    "online": True, "last_seen": time.time(), "location": {},
                    "peer": self.peer, "reg_info": {},
                }
            devices[self.phone]["authed"]    = True
            devices[self.phone]["online"]    = True
            devices[self.phone]["last_seen"] = time.time()
            active_jt808[self.phone]        = self  # register for JT1078 signaling
            log.info("Terminal %s authenticated successfully", self.phone)
        else:
            result = RESULT_FAILURE
            log.warning("Terminal %s authentication FAILED", self.phone)

        resp = build_general_resp(self.phone, hdr.serial, MSG_AUTH,
                                  result=result,
                                  serial=self._next_serial(),
                                  is_2019=self._is_2019)
        await self._send(resp)

        if result == RESULT_SUCCESS:
            await asyncio.sleep(2)
            # Query all device parameters
            qry = build_query_all_params(self.phone, self._next_serial(), is_2019=self._is_2019)
            await self._send(qry)
            await asyncio.sleep(2)
            # Request all 4 camera channels
            for ch in range(1, _NUM_CAMERAS + 1):
                await self._request_stream(channel=ch)
                await asyncio.sleep(0.5)

    async def _on_heartbeat(self, hdr):
        self.phone = hdr.phone

        # ── Auth-state enforcement ────────────────────────────────────────
        if not self._is_authed():
            log.warning("Heartbeat from unauthenticated terminal %s — rejecting", self.phone)
            resp = build_general_resp(self.phone, hdr.serial, MSG_HEARTBEAT,
                                      result=RESULT_FAILURE,
                                      serial=self._next_serial(),
                                      is_2019=self._is_2019)
            await self._send(resp)
            return

        if self.phone in devices:
            devices[self.phone]["last_seen"] = time.time()
            devices[self.phone]["online"]    = True

        log.debug("HEARTBEAT %s", self.phone)
        resp = build_general_resp(self.phone, hdr.serial, MSG_HEARTBEAT,
                                  result=RESULT_SUCCESS,
                                  serial=self._next_serial(),
                                  is_2019=self._is_2019)
        await self._send(resp)

        # Periodic re-request for all 4 channels every ~5 min
        self._heartbeat_count += 1
        if self._heartbeat_count % 50 == 0:
            async def refresh_all():
                for ch in range(1, _NUM_CAMERAS + 1):
                    await self._request_stream(channel=ch)
                    await asyncio.sleep(0.5)
            asyncio.ensure_future(refresh_all())

    async def _on_location(self, hdr, body: bytes):
        self.phone = hdr.phone

        # ── Auth-state enforcement ────────────────────────────────────────
        if not self._is_authed():
            log.warning("Location from unauthenticated terminal %s — rejecting", self.phone)
            resp = build_general_resp(self.phone, hdr.serial, MSG_LOCATION,
                                      result=RESULT_FAILURE,
                                      serial=self._next_serial(),
                                      is_2019=self._is_2019)
            await self._send(resp)
            return

        loc = parse_location(body)
        if loc:
            if self.phone in devices:
                devices[self.phone]["location"]  = {
                    "lat": loc.lat, "lon": loc.lon,
                    "speed_kmh": loc.speed_kmh, "direction": loc.direction,
                    "altitude": loc.altitude, "timestamp": str(loc.timestamp),
                    "alarm_flag": loc.alarm_flag, "status_flag": loc.status_flag,
                }
                devices[self.phone]["last_seen"] = time.time()

            log.info(
                "LOCATION %s | lat=%.6f lon=%.6f spd=%.1fkm/h dir=%d° alt=%dm time=%s",
                self.phone, loc.lat, loc.lon, loc.speed_kmh,
                loc.direction, loc.altitude, loc.timestamp,
            )
            if loc.alarm_flag:
                log.warning("ALARM FLAGS for %s: 0x%08X", self.phone, loc.alarm_flag)
            if loc.status_flag:
                log.debug("Status flags for %s: 0x%08X", self.phone, loc.status_flag)

        resp = build_general_resp(self.phone, hdr.serial, MSG_LOCATION,
                                  result=RESULT_SUCCESS,
                                  serial=self._next_serial(),
                                  is_2019=self._is_2019)
        await self._send(resp)

    async def _on_location_batch(self, hdr, body: bytes):
        """0x0704 — batch cached location upload."""
        self.phone = hdr.phone

        if not self._is_authed():
            log.warning("Batch location from unauthenticated %s — rejecting", self.phone)
            resp = build_general_resp(self.phone, hdr.serial, MSG_LOCATION_BATCH,
                                      result=RESULT_FAILURE,
                                      serial=self._next_serial(),
                                      is_2019=self._is_2019)
            await self._send(resp)
            return

        locations = parse_batch_location(body)
        log.info("BATCH LOCATION %s — %d items", self.phone, len(locations))
        for i, loc in enumerate(locations):
            log.info(
                "  [%d] lat=%.6f lon=%.6f spd=%.1fkm/h time=%s",
                i, loc.lat, loc.lon, loc.speed_kmh, loc.timestamp,
            )

        resp = build_general_resp(self.phone, hdr.serial, MSG_LOCATION_BATCH,
                                  result=RESULT_SUCCESS,
                                  serial=self._next_serial(),
                                  is_2019=self._is_2019)
        await self._send(resp)

    async def _on_logout(self, hdr):
        """0x0003 — terminal logout / de-register."""
        self.phone = hdr.phone
        log.info("LOGOUT %s", self.phone)

        if self.phone in devices:
            devices[self.phone]["authed"] = False
            devices[self.phone]["online"] = False

        auth_store.revoke(self.phone)

        resp = build_general_resp(self.phone, hdr.serial, MSG_TERMINAL_LOGOUT,
                                  result=RESULT_SUCCESS,
                                  serial=self._next_serial(),
                                  is_2019=self._is_2019)
        await self._send(resp)

    def _on_params_resp(self, body: bytes):
        """Parse 0x0104 — all device parameters response.
        Format: resp_serial(2) | count(1) | [param_id(4) | len(1) | value(len)] * count
        """
        if len(body) < 3:
            log.info("PARAMS 0x0104 — body too short (%d bytes): %s", len(body), body.hex())
            return
        resp_serial = struct.unpack_from(">H", body, 0)[0]
        count = body[2]
        offset = 3
        log.info("PARAMS 0x0104 — resp_serial=%d count=%d raw=%s", resp_serial, count, body.hex())
        for _ in range(count):
            if offset + 5 > len(body):
                break
            param_id  = struct.unpack_from(">I", body, offset)[0]
            param_len = body[offset + 4]
            offset += 5
            if offset + param_len > len(body):
                break
            value = body[offset:offset + param_len]
            offset += param_len
            try:
                val_str = value.decode("ascii", errors="replace").rstrip("\x00")
            except Exception:
                val_str = value.hex()
            log.info("  param 0x%08X len=%d: %r  hex=%s", param_id, param_len, val_str, value.hex())

    async def _on_media(self, hdr, mid: int):
        log.info("MEDIA msg 0x%04X from %s", mid, hdr.phone)
        resp = build_general_resp(hdr.phone, hdr.serial, mid,
                                  result=RESULT_SUCCESS,
                                  serial=self._next_serial(),
                                  is_2019=self._is_2019)
        await self._send(resp)

    async def _request_stream(self, channel: int = 1):
        """Send 0x9101 to open JT1078 stream for the given channel."""
        log.info("Requesting AV stream ch%d from %s → %s:%d",
                 channel, self.phone, self.server_ip, self.jt1078_port)
        cmd = build_av_request(
            phone=self.phone,
            serial=self._next_serial(),
            channel=channel,
            av_type=0,               # 0=audio+video, 1=video only
            stream_type=0,            # main stream
            server_ip=self.server_ip,
            tcp_port=self.jt1078_port,
            udp_port=0,
            is_2019=self._is_2019,
        )
        await self._send(cmd)


# ─────────────────────────────────────────────────────────────────────────────
#  JT1078  —  video/audio stream connection
# ─────────────────────────────────────────────────────────────────────────────

class JT1078Connection:
    def __init__(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter):
        self.reader    = reader
        self.writer    = writer
        addr           = writer.get_extra_info("peername")
        self.peer      = f"{addr[0]}:{addr[1]}" if addr else "?"
        self._buf      = StreamBuffer()
        self._assemblers: dict[int, FrameAssembler] = {}

    def _assembler(self, ch: int) -> FrameAssembler:
        if ch not in self._assemblers:
            self._assemblers[ch] = FrameAssembler()
        return self._assemblers[ch]

    async def run(self):
        # Identify device by matching the camera's IP to a known JT808 peer
        cam_ip = self.peer.split(":")[0]
        my_phone = None
        for phone, info in devices.items():
            if info.get("peer", "").startswith(cam_ip):
                my_phone = phone
                break

        mgr = get_device_streams(my_phone) if my_phone else get_device_streams("unknown")
        log.info("JT1078 connect from %s → device=%s", self.peer, my_phone)

        # Track which channels this connection carries so we can re-request on disconnect
        channels_seen: set[int] = set()
        try:
            while True:
                try:
                    data = await asyncio.wait_for(self.reader.read(65536), timeout=READ_TIMEOUT)
                except asyncio.TimeoutError:
                    log.warning("JT1078 read timeout for %s", self.peer)
                    break

                if not data:
                    break

                log.debug("JT1078 RX %d bytes from %s | hex: %s",
                          len(data), self.peer, data[:64].hex() + ("…" if len(data) > 64 else ""))

                self._buf.feed(data)
                for pkt in self._buf.packets():
                    ch = pkt.channel
                    key = (my_phone or "unknown", ch)

                    if ch not in channels_seen:
                        # Claim ownership — evicts any previous connection for this channel
                        channels_seen.add(ch)
                        _jt1078_owners[key] = self
                        log.info("JT1078 ch%d claimed by %s device=%s", ch, self.peer, my_phone)
                        await mgr.soft_reset(ch)

                    # Skip packets if a newer connection has evicted us for this channel
                    if _jt1078_owners.get(key) is not self:
                        continue

                    await self._handle_packet(pkt, mgr)

        except (asyncio.IncompleteReadError, ConnectionResetError, BrokenPipeError):
            pass
        finally:
            log.info("JT1078 disconnect: channels=%s device=%s", sorted(channels_seen), my_phone)
            for ch in sorted(channels_seen):
                key = (my_phone or "unknown", ch)
                # Only release ownership if we still hold it
                if _jt1078_owners.get(key) is self:
                    del _jt1078_owners[key]
                # Re-request the channel so the camera reconnects
                for conn in list(active_jt808.values()):
                    if conn._is_authed():
                        asyncio.ensure_future(conn._request_stream(channel=ch))
                        break

    async def _handle_packet(self, pkt, mgr=None):
        if not pkt.payload:
            return
        if pkt.codec not in ('H264', 'H265', 'PT98', 'PT99'):
            if pkt.is_audio:
                return
        if mgr is None:
            return
        # Use the channel number embedded in the packet — never guess or assign it
        await mgr.write_frame(pkt.channel, pkt.payload, codec=pkt.codec)


# ─────────────────────────────────────────────────────────────────────────────
#  HTTP  —  HLS files + player + status API
# ─────────────────────────────────────────────────────────────────────────────

PLAYER_HTML = """\
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>N9 Dashcam Live</title>
<style>
  *{box-sizing:border-box}
  body{font-family:system-ui,sans-serif;background:#0f0f0f;color:#eee;margin:0;display:flex;height:100vh;overflow:hidden}
  /* Left panel */
  #sidebar{width:220px;min-width:180px;background:#1a1a1a;border-right:1px solid #2a2a2a;display:flex;flex-direction:column;overflow:hidden}
  #sidebar h2{font-size:.75rem;font-weight:600;color:#666;text-transform:uppercase;letter-spacing:.08em;padding:16px 16px 8px}
  #device-list{flex:1;overflow-y:auto}
  .device-item{padding:12px 16px;cursor:pointer;border-bottom:1px solid #222;transition:background .15s}
  .device-item:hover{background:#252525}
  .device-item.active{background:#1e3a5f;border-left:3px solid #3b82f6}
  .device-item .did{font-size:.85rem;font-weight:500;color:#ddd}
  .device-item .dmeta{font-size:.72rem;color:#666;margin-top:2px}
  .online-dot{display:inline-block;width:7px;height:7px;border-radius:50%;background:#22c55e;margin-right:6px}
  /* Main area */
  #main{flex:1;display:flex;flex-direction:column;overflow:hidden;padding:16px;gap:12px}
  #main-header{display:flex;align-items:center;gap:12px}
  #main-header h1{font-size:1rem;font-weight:500;margin:0;color:#ccc}
  #selected-device{font-size:.8rem;color:#3b82f6;font-weight:500}
  .grid{display:grid;grid-template-columns:repeat(2,1fr);gap:10px;flex:1;overflow:hidden}
  .card{background:#1c1c1c;border-radius:8px;overflow:hidden;display:flex;flex-direction:column}
  .card-hdr{display:flex;align-items:center;padding:8px 10px;background:#222;font-size:.75rem;font-weight:500;color:#888;text-transform:uppercase;letter-spacing:.05em}
  .dot{display:inline-block;width:7px;height:7px;border-radius:50%;background:#444;margin-right:6px}
  .dot.live{background:#22c55e}
  .card img{width:100%;flex:1;object-fit:cover;background:#000;display:block}
  #no-device{color:#555;font-size:.9rem;text-align:center;padding:60px 20px}
</style>
</head>
<body>
<div id="sidebar">
  <h2>Devices</h2>
  <div id="device-list"><div style="padding:16px;font-size:.8rem;color:#555">No devices connected</div></div>
</div>
<div id="main">
  <div id="main-header">
    <h1>N9 Dashcam — Live View</h1>
    <span id="selected-device"></span>
  </div>
  <div class="grid" id="grid"><div id="no-device">Select a device from the left panel</div></div>
</div>
<script>
let currentPhone = null;
let knownDevices = {};

function deviceLabel(phone, info) {
  const plate = (info.reg_info?.license_plate || '').replace(/\\u0000/g,'').replace(/[^\\x20-\\x7E]/g,'').trim();
  const model = (info.reg_info?.terminal_model || '').trim();
  return plate || model || phone.slice(-8);
}

function showDevice(phone, info) {
  currentPhone = phone;
  document.getElementById('selected-device').textContent = deviceLabel(phone, info);
  document.querySelectorAll('.device-item').forEach(el => el.classList.toggle('active', el.dataset.phone === phone));
  const grid = document.getElementById('grid');
  grid.innerHTML = '';
  document.getElementById('no-device')?.remove();
  for (let ch = 1; ch <= 4; ch++) {
    const card = document.createElement('div');
    card.className = 'card';
    const streams = info.streams || {};
    const live = Object.entries(streams).find(([k,v]) => parseInt(k)===ch && v.live);
    card.innerHTML = `<div class="card-hdr"><span class="dot${live?' live':''}" id="dot${phone}-${ch}"></span>Channel ${ch}</div>
      <img src="/mjpeg/${encodeURIComponent(phone)}/${ch}" style="height:100%;object-fit:cover;background:#000"
           onerror="setTimeout(()=>{ this.src='/mjpeg/${encodeURIComponent(phone)}/${ch}?t='+Date.now(); },2000)">`;
    grid.appendChild(card);
  }
}

function renderSidebar(devicesData) {
  const list = document.getElementById('device-list');
  const phones = Object.keys(devicesData);
  if (!phones.length) {
    list.innerHTML = '<div style="padding:16px;font-size:.8rem;color:#555">No devices connected</div>';
    currentPhone = null;
    document.getElementById('grid').innerHTML = '<div id="no-device" style="color:#555;font-size:.9rem;text-align:center;padding:60px 20px">No devices connected</div>';
    document.getElementById('selected-device').textContent = '';
    return;
  }
  // Add new devices, remove gone ones
  phones.forEach(phone => {
    if (!document.querySelector(`[data-phone="${phone}"]`)) {
      const item = document.createElement('div');
      item.className = 'device-item';
      item.dataset.phone = phone;
      const info = devicesData[phone];
      item.innerHTML = `<div class="did"><span class="online-dot"></span>${deviceLabel(phone,info)}</div>
        <div class="dmeta">ID: ${phone.slice(-8)}</div>`;
      item.onclick = () => showDevice(phone, devicesData[phone]);
      list.appendChild(item);
    }
  });
  list.querySelectorAll('.device-item').forEach(el => {
    if (!devicesData[el.dataset.phone]) el.remove();
  });
  // Auto-select first device if none selected
  if (!currentPhone || !devicesData[currentPhone]) {
    showDevice(phones[0], devicesData[phones[0]]);
  }
}

function updateDots(devicesData) {
  if (!currentPhone || !devicesData[currentPhone]) return;
  const streams = devicesData[currentPhone].streams || {};
  for (let ch = 1; ch <= 4; ch++) {
    const dot = document.getElementById(`dot${currentPhone}-${ch}`);
    const live = Object.entries(streams).some(([k,v]) => parseInt(k)===ch && v.live);
    if (dot) dot.className = 'dot' + (live?' live':'');
  }
}

async function poll() {
  try {
    const d = await (await fetch('/status')).json();
    renderSidebar(d.devices || {});
    updateDots(d.devices || {});
  } catch(e) {}
  setTimeout(poll, 3000);
}
poll();
</script>
</body>
</html>
"""


async def http_handler(reader: asyncio.StreamReader, writer: asyncio.StreamWriter):
    """HTTP server — player, status API, and WebSocket upgrade for live video."""
    try:
        try:
            request_line = (await asyncio.wait_for(reader.readline(), timeout=10)).decode(errors="replace").strip()
        except asyncio.TimeoutError:
            return
        if not request_line:
            return

        # Drain headers, collecting Upgrade and WebSocket key
        headers = {}
        while True:
            try:
                line = await asyncio.wait_for(reader.readline(), timeout=5)
            except asyncio.TimeoutError:
                break
            if line in (b"\r\n", b"\n", b""):
                break
            if b":" in line:
                k, _, v = line.decode(errors="replace").partition(":")
                headers[k.strip().lower()] = v.strip()

        parts = request_line.split(" ")
        if len(parts) < 2:
            return
        path = parts[1].split("?")[0]

        # MJPEG live stream — /mjpeg/<phone>/<channel>
        if path.startswith("/mjpeg/"):
            parts_p = path.strip("/").split("/")
            try:
                phone = parts_p[1]
                ch = int(parts_p[2].split("?")[0])
            except (ValueError, IndexError):
                writer.close()
                return
            mgr = get_device_streams(phone)
            # Send MJPEG headers
            writer.write(
                b"HTTP/1.0 200 OK\r\n"
                b"Content-Type: multipart/x-mixed-replace; boundary=frame\r\n"
                b"Cache-Control: no-cache, no-store\r\n"
                b"Access-Control-Allow-Origin: *\r\n"
                b"\r\n"
            )
            await writer.drain()
            mgr.add_mjpeg_client(ch, writer)
            try:
                while True:
                    await asyncio.sleep(30)
                    # Send a keepalive comment (MJPEG doesn't need explicit keepalive, but doesn't hurt)
            except Exception:
                pass
            finally:
                mgr.remove_mjpeg_client(ch, writer)
                try:
                    writer.close()
                except Exception:
                    pass
            return

        static_dir = Path(__file__).parent / "static"

        def respond(status: str, ct: str, body: bytes):
            hdr = (
                f"HTTP/1.0 {status}\r\n"
                f"Content-Type: {ct}\r\n"
                f"Content-Length: {len(body)}\r\n"
                f"Access-Control-Allow-Origin: *\r\n"
                f"Cache-Control: no-cache\r\n\r\n"
            ).encode()
            writer.write(hdr + body)

        if path in ("/", "/player"):
            respond("200 OK", "text/html", PLAYER_HTML.encode())

        elif path == "/status":
            payload = json.dumps({
                "devices": {
                    phone: {
                        **info,
                        "streams": get_device_streams(phone).status,
                    }
                    for phone, info in devices.items()
                }
            }, indent=2, default=str).encode()
            respond("200 OK", "application/json", payload)

        else:
            respond("404 Not Found", "text/plain", b"Not found")

        try:
            await asyncio.wait_for(writer.drain(), timeout=WRITE_TIMEOUT)
        except asyncio.TimeoutError:
            pass

    except Exception as e:
        log.debug("HTTP error: %s", e)
    finally:
        writer.close()


# ─────────────────────────────────────────────────────────────────────────────
#  Entry point
# ─────────────────────────────────────────────────────────────────────────────

async def main():
    parser = argparse.ArgumentParser(description="N9 Dashcam JT808/JT1078/HLS Server")
    parser.add_argument("--808-port",  type=int, default=6608,  dest="port_808")
    parser.add_argument("--1078-port", type=int, default=1078,  dest="port_1078")
    parser.add_argument("--http-port", type=int, default=8080,  dest="port_http")
    parser.add_argument("--host",      type=str, default="0.0.0.0")
    parser.add_argument("--server-ip", type=str, default="",
                        help="Public IP this server is reachable at (sent to device).")
    args = parser.parse_args()

    server_ip = args.server_ip
    if not server_ip:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as s:
            try:
                s.connect(("8.8.8.8", 80))
                server_ip = s.getsockname()[0]
            except Exception:
                server_ip = "127.0.0.1"
        log.info("Auto-detected server IP: %s (override with --server-ip)", server_ip)

    async def jt808_dispatcher(reader: asyncio.StreamReader,
                               writer: asyncio.StreamWriter):
        """
        Combined JT808 + JT1078 handler on the same port.
        Detects protocol by first 4 bytes: JT1078 magic vs JT808 0x7E.
        """
        from jt1078 import MAGIC as JT1078_MAGIC
        try:
            first4 = await asyncio.wait_for(reader.read(4), timeout=10)
        except asyncio.TimeoutError:
            writer.close()
            return
        if not first4:
            writer.close()
            return
        reader.feed_data(first4)   # put the bytes back
        if first4 == JT1078_MAGIC:
            await JT1078Connection(reader, writer).run()
        else:
            await JT808Connection(reader, writer, server_ip, args.port_1078).run()

    jt808_srv = await asyncio.start_server(
        jt808_dispatcher,
        host=args.host, port=args.port_808,
    )
    jt1078_srv = await asyncio.start_server(
        lambda r, w: JT1078Connection(r, w).run(),
        host=args.host, port=args.port_1078,
    )
    http_srv = await asyncio.start_server(
        http_handler,
        host=args.host, port=args.port_http,
    )

    log.info("=" * 60)
    log.info("JT808  listening on  %s:%d", args.host, args.port_808)
    log.info("JT1078 listening on  %s:%d", args.host, args.port_1078)
    log.info("HTTP   listening on  http://%s:%d", server_ip, args.port_http)
    log.info("Player at            http://%s:%d/", server_ip, args.port_http)
    log.info("=" * 60)
    log.info("Point N9 to → IP: %s  Port: %d  Protocol: JT808_19",
             server_ip, args.port_808)

    async with jt808_srv, jt1078_srv, http_srv:
        await asyncio.gather(
            jt808_srv.serve_forever(),
            jt1078_srv.serve_forever(),
            http_srv.serve_forever(),
        )


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        log.info("Shutting down")