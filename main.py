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
import time
import socket
from pathlib import Path

from jt808 import (
    parse_frame,
    build_general_resp, build_register_resp, build_av_request, build_av_close,
    parse_register_info, parse_location, parse_batch_location,
    AuthCodeStore,
    MSG_REGISTER, MSG_AUTH, MSG_HEARTBEAT, MSG_LOCATION,
    MSG_LOCATION_BATCH, MSG_TERMINAL_LOGOUT,
    MSG_MEDIA_INFO, MSG_MEDIA_UPLOAD,
    MSG_REGISTER_RESP, MSG_GENERAL_RESP,
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
READ_TIMEOUT  = 60    # how long to wait for next data from device
WRITE_TIMEOUT = 10    # how long a send is allowed to take

# ── Shared state ─────────────────────────────────────────────────────────────
stream_mgr  = StreamManager()
auth_store  = AuthCodeStore()

# phone -> {phone, registered, authed, last_seen, location, peer, reg_info}
devices: dict[str, dict] = {}


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
            async with asyncio.timeout(WRITE_TIMEOUT):
                self.writer.write(data)
                await self.writer.drain()
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
                    async with asyncio.timeout(READ_TIMEOUT):
                        chunk = await self.reader.read(4096)
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
                dev = devices.get(self.phone, {})
                dev["online"] = False
                dev["authed"] = False
                auth_store.revoke(self.phone)

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
        else:
            log.debug("Unsupported msg 0x%04X from %s", mid, hdr.phone)
            resp = build_general_resp(hdr.phone, hdr.serial, mid,
                                      result=RESULT_NOT_SUPPORTED,
                                      serial=self._next_serial())
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
                                   auth_code=auth_code)
        await self._send(resp)

    async def _on_auth(self, hdr, body: bytes):
        self.phone     = hdr.phone
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
            log.info("Terminal %s authenticated successfully", self.phone)
        else:
            result = RESULT_FAILURE
            log.warning("Terminal %s authentication FAILED", self.phone)

        resp = build_general_resp(self.phone, hdr.serial, MSG_AUTH,
                                  result=result,
                                  serial=self._next_serial())
        await self._send(resp)

        # Only request video if auth succeeded
        if result == RESULT_SUCCESS:
            await asyncio.sleep(0.5)
            await self._request_stream(channel=1)

    async def _on_heartbeat(self, hdr):
        self.phone = hdr.phone

        # ── Auth-state enforcement ────────────────────────────────────────
        if not self._is_authed():
            log.warning("Heartbeat from unauthenticated terminal %s — rejecting", self.phone)
            resp = build_general_resp(self.phone, hdr.serial, MSG_HEARTBEAT,
                                      result=RESULT_FAILURE,
                                      serial=self._next_serial())
            await self._send(resp)
            return

        if self.phone in devices:
            devices[self.phone]["last_seen"] = time.time()
            devices[self.phone]["online"]    = True

        log.debug("HEARTBEAT %s", self.phone)
        resp = build_general_resp(self.phone, hdr.serial, MSG_HEARTBEAT,
                                  result=RESULT_SUCCESS,
                                  serial=self._next_serial())
        await self._send(resp)

    async def _on_location(self, hdr, body: bytes):
        self.phone = hdr.phone

        # ── Auth-state enforcement ────────────────────────────────────────
        if not self._is_authed():
            log.warning("Location from unauthenticated terminal %s — rejecting", self.phone)
            resp = build_general_resp(self.phone, hdr.serial, MSG_LOCATION,
                                      result=RESULT_FAILURE,
                                      serial=self._next_serial())
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
                                  serial=self._next_serial())
        await self._send(resp)

    async def _on_location_batch(self, hdr, body: bytes):
        """0x0704 — batch cached location upload."""
        self.phone = hdr.phone

        if not self._is_authed():
            log.warning("Batch location from unauthenticated %s — rejecting", self.phone)
            resp = build_general_resp(self.phone, hdr.serial, MSG_LOCATION_BATCH,
                                      result=RESULT_FAILURE,
                                      serial=self._next_serial())
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
                                  serial=self._next_serial())
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
                                  serial=self._next_serial())
        await self._send(resp)

    async def _on_media(self, hdr, mid: int):
        log.info("MEDIA msg 0x%04X from %s", mid, hdr.phone)
        resp = build_general_resp(hdr.phone, hdr.serial, mid,
                                  result=RESULT_SUCCESS,
                                  serial=self._next_serial())
        await self._send(resp)

    async def _request_stream(self, channel: int = 1):
        """Send 0x9101 — tell device to push video to our JT1078 port."""
        log.info("Requesting AV stream ch%d from %s → %s:%d",
                 channel, self.phone, self.server_ip, self.jt1078_port)
        cmd = build_av_request(
            phone=self.phone,
            serial=self._next_serial(),
            channel=channel,
            av_type=1,               # video only (0 = audio+video)
            stream_type=0,            # main stream
            server_ip=self.server_ip,
            tcp_port=self.jt1078_port,
            udp_port=0,
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
        log.info("JT1078 connect from %s", self.peer)
        try:
            while True:
                try:
                    async with asyncio.timeout(READ_TIMEOUT):
                        data = await self.reader.read(65536)
                except asyncio.TimeoutError:
                    log.warning("JT1078 read timeout for %s", self.peer)
                    break

                if not data:
                    break

                log.debug("JT1078 RX %d bytes from %s | hex: %s",
                          len(data), self.peer, data[:64].hex() + ("…" if len(data) > 64 else ""))

                self._buf.feed(data)
                for pkt in self._buf.packets():
                    await self._handle_packet(pkt)

        except (asyncio.IncompleteReadError, ConnectionResetError, BrokenPipeError):
            pass
        finally:
            log.info("JT1078 disconnect: %s", self.peer)

    async def _handle_packet(self, pkt):
        if not pkt.is_video:
            return
        frame = self._assembler(pkt.channel).feed(pkt)
        if frame:
            if not frame.startswith(b'\x00\x00\x00\x01'):
                frame = b'\x00\x00\x00\x01' + frame
            await stream_mgr.write_frame(pkt.channel, frame)


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
  body { font-family: system-ui, sans-serif; background: #111; color: #eee; margin: 0; padding: 20px; }
  h1   { font-size: 1.2rem; font-weight: 500; margin: 0 0 1rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(400px, 1fr)); gap: 16px; }
  .card { background: #1c1c1c; border-radius: 8px; padding: 12px; }
  .card h2 { font-size: 0.85rem; font-weight: 500; color: #888; margin: 0 0 8px; text-transform: uppercase; letter-spacing: .05em; }
  video { width: 100%; border-radius: 4px; background: #000; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #555; margin-right: 6px; }
  .dot.live { background: #22c55e; }
  pre { font-size: .75rem; color: #888; overflow: auto; max-height: 200px; background: #0a0a0a; padding: 8px; border-radius: 4px; }
</style>
<script src="https://cdn.jsdelivr.net/npm/hls.js@1.5.7/dist/hls.min.js"></script>
</head>
<body>
<h1>N9 Dashcam — Live View</h1>
<div class="grid" id="grid"></div>
<div class="card" style="margin-top:16px"><h2>Device status</h2><pre id="status">loading...</pre></div>
<script>
const BASE = window.location.origin;
function createPlayer(ch) {
  const card = document.createElement('div');
  card.className = 'card';
  card.innerHTML = `<h2><span class="dot" id="dot${ch}"></span>Channel ${ch}</h2>
    <video id="v${ch}" controls autoplay muted playsinline></video>`;
  document.getElementById('grid').appendChild(card);
  const url = `${BASE}/hls/${ch}/index.m3u8`;
  const v = document.getElementById(`v${ch}`);
  if (Hls.isSupported()) {
    const hls = new Hls({ lowLatencyMode: true });
    hls.loadSource(url); hls.attachMedia(v);
    hls.on(Hls.Events.MANIFEST_PARSED, () => v.play());
    hls.on(Hls.Events.ERROR, (e, d) => { if (d.fatal) setTimeout(() => hls.loadSource(url), 3000); });
  } else if (v.canPlayType('application/vnd.apple.mpegurl')) { v.src = url; }
}
[1,2,3,4].forEach(createPlayer);
async function poll() {
  try {
    const d = await (await fetch('/status')).json();
    document.getElementById('status').textContent = JSON.stringify(d, null, 2);
    for (const [ch, info] of Object.entries(d.streams || {})) {
      const dot = document.getElementById('dot'+ch);
      if (dot) dot.className = 'dot'+(info.live?' live':'');
    }
  } catch(e) {}
  setTimeout(poll, 3000);
}
poll();
</script>
</body>
</html>
"""


async def http_handler(reader: asyncio.StreamReader, writer: asyncio.StreamWriter):
    """Minimal HTTP/1.0 server — HLS files + status + player."""
    try:
        try:
            async with asyncio.timeout(10):
                request_line = await reader.readline()
        except asyncio.TimeoutError:
            return

        request_line = request_line.decode(errors="replace").strip()
        if not request_line:
            return

        # Drain headers
        while True:
            try:
                async with asyncio.timeout(5):
                    line = await reader.readline()
            except asyncio.TimeoutError:
                break
            if line in (b"\r\n", b"\n", b""):
                break

        parts = request_line.split(" ")
        if len(parts) < 2:
            return
        path = parts[1].split("?")[0]

        static_dir = Path(__file__).parent.parent / "static"

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
                "devices": devices,
                "streams": stream_mgr.status(),
            }, indent=2, default=str).encode()
            respond("200 OK", "application/json", payload)

        elif path.startswith("/hls/"):
            fpath = static_dir / "hls" / path[5:]
            if fpath.exists() and fpath.is_file():
                ct = ("application/vnd.apple.mpegurl"
                      if str(fpath).endswith(".m3u8") else "video/mp2t")
                respond("200 OK", ct, fpath.read_bytes())
            else:
                respond("404 Not Found", "text/plain", b"Not found")

        else:
            respond("404 Not Found", "text/plain", b"Not found")

        try:
            async with asyncio.timeout(WRITE_TIMEOUT):
                await writer.drain()
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

    jt808_srv = await asyncio.start_server(
        lambda r, w: JT808Connection(r, w, server_ip, args.port_1078).run(),
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