"""
JT/T 808-2019 protocol parser.

Frame format:
  0x7E | header (12 bytes) | body | checksum (1 byte) | 0x7E

Escape rules (applied AFTER stripping start/end 0x7E):
  0x7D 0x02  ->  0x7E
  0x7D 0x01  ->  0x7D

New vs original:
  - Dynamic auth code generation + AuthCodeStore for validation
  - TerminalRegisterInfo dataclass + parse_register_info()
  - LocationInfo dataclass with proper datetime
  - parse_batch_location() for 0x0704
  - MSG_TERMINAL_LOGOUT (0x0003)
  - RESULT_* constants matching Go server
"""

import struct
import logging
import secrets
from dataclasses import dataclass
from typing import Optional
from datetime import datetime

log = logging.getLogger("jt808")

# ── Message IDs (device -> server) ──────────────────────────────────────────
MSG_REGISTER        = 0x0100
MSG_AUTH            = 0x0102
MSG_HEARTBEAT       = 0x0002
MSG_LOCATION        = 0x0200
MSG_LOCATION_BATCH  = 0x0704   # batch cached location upload
MSG_TERMINAL_LOGOUT = 0x0003   # terminal logout / de-register
MSG_MEDIA_INFO      = 0x1003
MSG_MEDIA_UPLOAD    = 0x1005

# ── Message IDs (server -> device) ──────────────────────────────────────────
MSG_REGISTER_RESP   = 0x8100
MSG_GENERAL_RESP    = 0x8001
MSG_AV_REQ          = 0x9101
MSG_AV_CLOSE        = 0x9102

FRAME_MARKER = 0x7E

# ── Result codes (matches Go RESULT_* constants) ─────────────────────────────
RESULT_SUCCESS       = 0x00
RESULT_FAILURE       = 0x01
RESULT_MESSAGE_ERROR = 0x02
RESULT_NOT_SUPPORTED = 0x03
RESULT_ALARM_CONFIRM = 0x04


# ─────────────────────────────────────────────────────────────────────────────
#  Data classes
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class JT808Header:
    msg_id:      int
    body_props:  int
    phone:       str    # hex string e.g. "088556000002"
    serial:      int
    pkg_total:   int = 0
    pkg_index:   int = 0

    @property
    def has_subpkg(self) -> bool:
        return bool(self.body_props & (1 << 13))

    @property
    def body_len(self) -> int:
        return self.body_props & 0x03FF


@dataclass
class JT808Message:
    header: JT808Header
    body:   bytes
    valid:  bool = True


@dataclass
class TerminalRegisterInfo:
    """Parsed body of 0x0100 registration message — mirrors Go's TerminalRegisterInfo."""
    province_id:     int
    city_id:         int
    manufacturer_id: str
    terminal_model:  str
    terminal_id:     str
    license_color:   int
    license_plate:   str


@dataclass
class LocationInfo:
    """Parsed body of 0x0200 / batch location item — mirrors Go's LocationInfo."""
    alarm_flag:  int
    status_flag: int
    lat:         float
    lon:         float
    altitude:    int
    speed_kmh:   float
    direction:   int
    timestamp:   datetime


# ─────────────────────────────────────────────────────────────────────────────
#  Frame codec
# ─────────────────────────────────────────────────────────────────────────────

def unescape(data: bytes) -> bytes:
    """Reverse JT808 byte-stuffing."""
    out = bytearray()
    i = 0
    while i < len(data):
        if data[i] == 0x7D:
            if i + 1 >= len(data):
                break
            nxt = data[i + 1]
            if nxt == 0x02:
                out.append(0x7E)
            elif nxt == 0x01:
                out.append(0x7D)
            else:
                out.append(data[i])
                out.append(nxt)
            i += 2
        else:
            out.append(data[i])
            i += 1
    return bytes(out)


def escape(data: bytes) -> bytes:
    """Apply JT808 byte-stuffing."""
    out = bytearray()
    for b in data:
        if b == 0x7E:
            out += b'\x7D\x02'
        elif b == 0x7D:
            out += b'\x7D\x01'
        else:
            out.append(b)
    return bytes(out)


def checksum(data: bytes) -> int:
    """XOR checksum over all bytes."""
    cs = 0
    for b in data:
        cs ^= b
    return cs


def parse_frame(raw: bytes) -> Optional[JT808Message]:
    """Parse a single JT808 frame including outer 0x7E markers."""
    if len(raw) < 2 or raw[0] != FRAME_MARKER or raw[-1] != FRAME_MARKER:
        log.debug("Bad frame markers: %s", raw.hex())
        return None

    inner = unescape(raw[1:-1])
    if len(inner) < 13:
        log.debug("Frame too short: %d bytes", len(inner))
        return None

    cs_expected = inner[-1]
    cs_actual   = checksum(inner[:-1])
    if cs_expected != cs_actual:
        log.warning("Checksum mismatch: expected 0x%02X got 0x%02X", cs_expected, cs_actual)
        return None

    payload    = inner[:-1]
    msg_id     = struct.unpack_from(">H", payload, 0)[0]
    body_props = struct.unpack_from(">H", payload, 2)[0]
    phone      = payload[4:10].hex()
    serial     = struct.unpack_from(">H", payload, 10)[0]

    has_sub = bool(body_props & (1 << 13))
    if has_sub:
        if len(payload) < 16:
            return None
        pkg_total = struct.unpack_from(">H", payload, 12)[0]
        pkg_index = struct.unpack_from(">H", payload, 14)[0]
        body = payload[16:]
    else:
        pkg_total = pkg_index = 0
        body = payload[12:]

    header = JT808Header(
        msg_id=msg_id, body_props=body_props, phone=phone,
        serial=serial, pkg_total=pkg_total, pkg_index=pkg_index,
    )
    return JT808Message(header=header, body=body)


def build_frame(msg_id: int, phone_hex: str, serial: int, body: bytes) -> bytes:
    """Build a server→device JT808 frame."""
    phone_bytes = bytes.fromhex(phone_hex.zfill(12))
    body_props  = len(body) & 0x03FF
    header      = struct.pack(">HH", msg_id, body_props) + phone_bytes + struct.pack(">H", serial)
    payload     = header + body
    cs          = checksum(payload)
    full        = payload + bytes([cs])
    return bytes([FRAME_MARKER]) + escape(full) + bytes([FRAME_MARKER])


# ─────────────────────────────────────────────────────────────────────────────
#  Response / command builders
# ─────────────────────────────────────────────────────────────────────────────

def build_general_resp(phone: str, resp_serial: int, resp_msg_id: int,
                       result: int = RESULT_SUCCESS, serial: int = 0) -> bytes:
    """0x8001 General response."""
    body = struct.pack(">HHB", resp_serial, resp_msg_id, result)
    return build_frame(MSG_GENERAL_RESP, phone, serial, body)


def build_register_resp(phone: str, serial: int,
                        result: int = RESULT_SUCCESS,
                        auth_code: str = "") -> bytes:
    """
    0x8100 Registration response.
    Pass auth_code="" to have one generated dynamically (recommended).
    """
    auth_bytes = auth_code.encode("ascii")
    body = struct.pack(">HB", serial, result) + auth_bytes
    return build_frame(MSG_REGISTER_RESP, phone, 0, body)


def build_av_request(phone: str, serial: int, channel: int = 1,
                     av_type: int = 1, stream_type: int = 0,
                     server_ip: str = "0.0.0.0", tcp_port: int = 0,
                     udp_port: int = 0) -> bytes:
    """0x9101 Real-time AV stream request."""
    ip_bytes = server_ip.encode("ascii").ljust(31, b'\x00')[:31]
    body = (
        ip_bytes
        + struct.pack(">H", tcp_port)
        + struct.pack(">H", udp_port)
        + struct.pack("B", channel)
        + struct.pack("B", av_type)
        + struct.pack("B", stream_type)
    )
    return build_frame(MSG_AV_REQ, phone, serial, body)


def build_av_close(phone: str, serial: int, channel: int = 1,
                   close_type: int = 0) -> bytes:
    """0x9102 Close AV stream."""
    body = struct.pack("BB", channel, close_type)
    return build_frame(MSG_AV_CLOSE, phone, serial, body)


# ─────────────────────────────────────────────────────────────────────────────
#  Body parsers
# ─────────────────────────────────────────────────────────────────────────────

def parse_register_info(body: bytes) -> Optional[TerminalRegisterInfo]:
    """
    Parse 0x0100 registration body (JT808-2019 layout):
      ProvinceID   2 bytes
      CityID       2 bytes
      ManufID      5 bytes ASCII
      TermModel   20 bytes ASCII null-padded
      TermID       7 bytes ASCII
      LicenseColor 1 byte
      LicensePlate variable GBK
    """
    if len(body) < 37:
        log.warning("Register body too short: %d bytes", len(body))
        return None
    try:
        province_id = struct.unpack_from(">H", body, 0)[0]
        city_id     = struct.unpack_from(">H", body, 2)[0]
        manuf_id    = body[4:9].decode("ascii",  errors="replace").rstrip("\x00").strip()
        term_model  = body[9:29].decode("ascii",  errors="replace").rstrip("\x00").strip()
        term_id     = body[29:36].decode("ascii", errors="replace").rstrip("\x00").strip()
        lic_color   = body[36]
        lic_plate   = body[37:].decode("gbk", errors="replace").rstrip("\x00") if len(body) > 37 else ""
        return TerminalRegisterInfo(
            province_id=province_id, city_id=city_id,
            manufacturer_id=manuf_id, terminal_model=term_model,
            terminal_id=term_id, license_color=lic_color,
            license_plate=lic_plate,
        )
    except Exception as e:
        log.warning("parse_register_info error: %s", e)
        return None


def parse_location(body: bytes) -> Optional[LocationInfo]:
    """
    Parse 0x0200 location body.
    Alarm(4) Status(4) Lat(4) Lon(4) Alt(2) Speed(2) Dir(2) Time(6 BCD)
    """
    if len(body) < 28:
        return None
    try:
        alarm_flag  = struct.unpack_from(">I", body, 0)[0]
        status_flag = struct.unpack_from(">I", body, 4)[0]
        lat         = struct.unpack_from(">I", body, 8)[0]  / 1e6
        lon         = struct.unpack_from(">I", body, 12)[0] / 1e6
        altitude    = struct.unpack_from(">H", body, 16)[0]
        speed       = struct.unpack_from(">H", body, 18)[0] / 10.0
        direction   = struct.unpack_from(">H", body, 20)[0]
        # BCD time at offset 22: YY MM DD HH mm ss (each nibble = one decimal digit)
        t = body[22:28].hex()  # e.g. "250430143000" = 2025-04-30 14:30:00
        ts = datetime(
            year   = 2000 + int(t[0:2], 10),
            month  = max(1, int(t[2:4], 10)),
            day    = max(1, int(t[4:6], 10)),
            hour   = int(t[6:8], 10),
            minute = int(t[8:10], 10),
            second = int(t[10:12], 10),
        )
        return LocationInfo(
            alarm_flag=alarm_flag, status_flag=status_flag,
            lat=lat, lon=lon, altitude=altitude,
            speed_kmh=speed, direction=direction, timestamp=ts,
        )
    except Exception as e:
        log.warning("parse_location error: %s", e)
        return None


def parse_batch_location(body: bytes) -> list:
    """
    Parse 0x0704 batch cached location report.
    Layout: item_count(2) | location_type(1) |
            [item_length(2) | location_body(N)] * item_count
    Returns list of LocationInfo.
    """
    if len(body) < 3:
        return []

    item_count    = struct.unpack_from(">H", body, 0)[0]
    location_type = body[2]
    log.info("Batch location: %d items, type=%d", item_count, location_type)

    locations = []
    offset = 3
    for i in range(item_count):
        if offset + 2 > len(body):
            break
        item_len = struct.unpack_from(">H", body, offset)[0]
        offset += 2
        if offset + item_len > len(body):
            log.warning("Batch item %d truncated", i)
            break
        loc = parse_location(body[offset: offset + item_len])
        if loc:
            locations.append(loc)
        offset += item_len

    return locations


# ─────────────────────────────────────────────────────────────────────────────
#  Auth code store  (dynamic codes, validated on AUTH)
# ─────────────────────────────────────────────────────────────────────────────

def _generate_auth_code(phone: str) -> str:
    """
    Dynamic auth code — unique per registration attempt.
    Format: AUTH_<last6ofphone>_<4randomHEX>
    Mirrors Go: fmt.Sprintf("AUTH_%s_%d", phone, time.Now().Unix()%10000)
    """
    suffix = secrets.token_hex(2).upper()
    short  = phone[-6:] if len(phone) >= 6 else phone
    return f"AUTH_{short}_{suffix}"


class AuthCodeStore:
    """
    Issues and validates dynamic auth codes.
    asyncio is single-threaded so no lock needed.
    """

    def __init__(self):
        self._codes: dict[str, str] = {}   # phone -> expected auth code

    def issue(self, phone: str) -> str:
        """Generate + store a new auth code for this phone. Returns the code."""
        code = _generate_auth_code(phone)
        self._codes[phone] = code
        log.debug("Auth code issued for %s: %s", phone, code)
        return code

    def validate(self, phone: str, presented: str) -> bool:
        """Return True if the presented code matches what we issued."""
        presented = presented.strip("\x00").strip()
        expected  = self._codes.get(phone, "")
        ok = bool(expected) and (presented == expected)
        if ok:
            log.info("Auth validated for %s", phone)
        else:
            log.warning("Auth FAILED for %s — got %r expected %r", phone, presented, expected)
        return ok

    def revoke(self, phone: str):
        """Remove auth code (on logout or disconnect)."""
        self._codes.pop(phone, None)
        log.debug("Auth code revoked for %s", phone)