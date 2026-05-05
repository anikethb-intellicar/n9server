"""
JT/T 1078-2016 RTP-like packet parser.

The N9 sends video over a TCP connection to a port you specify in the 0x9101 command.
Each packet is:

  0x30 0x31 0x63 0x64  (4-byte magic)
  V (4 bits) | P (1) | X (1) | CC (4 bits) | M (1) | PT (7 bits)
  Sequence number  (16 bits, big-endian)
  Timestamp        (32 bits, big-endian)  -- 90kHz clock
  SSRC             (32 bits)
  SIM              (6 bytes BCD)  -- device SIM / phone number
  Channel          (1 byte)
  Data type        (4 bits) | Packet type (4 bits)
  Timestamp ext    (8 bytes, unix ms)  -- JT1078 extension
  Last I-frame distance (16 bits)
  Payload length   (16 bits)
  Payload          (variable)

Total fixed header: 4 + 2 + 2 + 4 + 4 + 6 + 1 + 1 + 8 + 2 + 2 = 36 bytes
"""

import struct
import logging
from dataclasses import dataclass
from typing import Optional, Tuple

log = logging.getLogger("jt1078")

MAGIC = b'\x30\x31\x63\x64'

# Data types (upper 4 bits of the data_type byte)
DT_VIDEO    = 0x0   # video frame
DT_AUDIO    = 0x1   # audio frame
DT_SUBTITLE = 0x2

# Packet types (lower 4 bits of the data_type byte)
PT_RAW          = 0x0   # non-fragmented
PT_FIRST_FRAG   = 0x1   # first fragment
PT_LAST_FRAG    = 0x2   # last fragment
PT_MIDDLE_FRAG  = 0x3   # middle fragment

# RTP payload type -> codec
PAYLOAD_TYPE = {
    98:  "H264",
    99:  "H265",
    4:   "G711A",
    8:   "G711U",
    26:  "JPEG",
}

HEADER_SIZE = 36


@dataclass
class JT1078Packet:
    sim:          str        # hex BCD device ID
    channel:      int
    data_type:    int        # DT_VIDEO / DT_AUDIO / ...
    packet_type:  int        # PT_RAW / PT_FIRST_FRAG / ...
    codec:        str        # "H264", "H265", "G711A", ...
    timestamp_ms: int        # unix milliseconds
    sequence:     int
    payload:      bytes

    @property
    def is_video(self) -> bool:
        return self.data_type == DT_VIDEO

    @property
    def is_audio(self) -> bool:
        return self.data_type == DT_AUDIO

    @property
    def is_keyframe(self) -> bool:
        # In JT1078 the M bit in the RTP-like header signals I-frame
        # We track this via the codec-level; a simpler heuristic:
        # H264 keyframes start with 0x65 or 0x67 NAL after Annex-B prefix
        if not self.is_video or len(self.payload) < 5:
            return False
        # Skip possible 0x00 0x00 0x00 0x01 Annex-B prefix
        start = 4 if self.payload[:4] == b'\x00\x00\x00\x01' else 0
        nal_type = self.payload[start] & 0x1F
        return nal_type in (5, 7)  # IDR or SPS (= keyframe boundary)


def parse_packet(data: bytes) -> Optional[JT1078Packet]:
    """
    Parse a single JT1078 packet from raw bytes.
    Returns None if data is too short or magic doesn't match.
    """
    if len(data) < HEADER_SIZE:
        return None

    if data[:4] != MAGIC:
        log.debug("Bad magic: %s", data[:4].hex())
        return None

    # Byte 4-5: V(4)|P(1)|X(1)|CC(4) | M(1)|PT(7)
    b4, b5 = data[4], data[5]
    pt = b5 & 0x7F            # payload type (codec hint)

    sequence   = struct.unpack_from(">H", data, 6)[0]
    # timestamp  = struct.unpack_from(">I", data, 8)[0]   # 90kHz, unused
    # ssrc       = struct.unpack_from(">I", data, 12)[0]

    sim_bytes  = data[16:22]
    sim        = sim_bytes.hex()
    channel    = data[22]
    dt_byte    = data[23]
    data_type  = (dt_byte >> 4) & 0x0F
    pkt_type   = dt_byte & 0x0F

    # JT1078 extension: 8-byte unix millisecond timestamp at offset 24
    ts_ms = struct.unpack_from(">Q", data, 24)[0]

    # last_iframe_dist = struct.unpack_from(">H", data, 32)[0]  # informational
    payload_len = struct.unpack_from(">H", data, 34)[0]

    if len(data) < HEADER_SIZE + payload_len:
        log.debug("Packet truncated: have %d need %d", len(data), HEADER_SIZE + payload_len)
        return None

    payload = data[HEADER_SIZE: HEADER_SIZE + payload_len]
    codec   = PAYLOAD_TYPE.get(pt, f"PT{pt}")

    return JT1078Packet(
        sim=sim,
        channel=channel,
        data_type=data_type,
        packet_type=pkt_type,
        codec=codec,
        timestamp_ms=ts_ms,
        sequence=sequence,
        payload=payload,
    )


class FrameAssembler:
    """
    Re-assembles fragmented JT1078 video/audio frames.
    Fragments arrive as PT_FIRST_FRAG -> PT_MIDDLE_FRAG* -> PT_LAST_FRAG.
    Complete (non-fragmented) frames have PT_RAW.
    Yields complete frames as bytes.
    """

    def __init__(self):
        self._buf: bytearray = bytearray()
        self._assembling: bool = False

    def feed(self, pkt: JT1078Packet) -> Optional[bytes]:
        """
        Feed a packet. Returns a complete frame bytes if one is ready, else None.
        """
        pt = pkt.packet_type

        if pt == PT_RAW:
            self._buf.clear()
            self._assembling = False
            return bytes(pkt.payload)

        if pt == PT_FIRST_FRAG:
            self._buf = bytearray(pkt.payload)
            self._assembling = True
            return None

        if pt in (PT_MIDDLE_FRAG, PT_LAST_FRAG):
            if not self._assembling:
                log.warning("Got fragment without first fragment — dropping")
                return None
            self._buf += pkt.payload
            if pt == PT_LAST_FRAG:
                frame = bytes(self._buf)
                self._buf.clear()
                self._assembling = False
                return frame
            return None

        log.warning("Unknown packet type 0x%X", pt)
        return None


class StreamBuffer:
    """
    Accumulates raw TCP bytes and yields complete JT1078 packets.
    The magic 0x30316364 is used as a re-sync marker.
    """

    def __init__(self):
        self._raw = bytearray()

    def feed(self, data: bytes):
        self._raw += data

    def packets(self):
        """Generator — yields parsed JT1078Packet objects as they become available."""
        while True:
            # Find magic
            idx = self._raw.find(MAGIC)
            if idx == -1:
                # No magic found — keep last 3 bytes in case magic straddles chunks
                self._raw = self._raw[-3:] if len(self._raw) >= 3 else self._raw
                return

            if idx > 0:
                log.debug("Discarding %d bytes before magic", idx)
                self._raw = self._raw[idx:]

            if len(self._raw) < HEADER_SIZE:
                return  # wait for more data

            # Peek at payload length to know full packet size
            payload_len = struct.unpack_from(">H", self._raw, 34)[0]
            total = HEADER_SIZE + payload_len

            if len(self._raw) < total:
                return  # wait for more data

            raw_pkt = bytes(self._raw[:total])
            self._raw = self._raw[total:]

            pkt = parse_packet(raw_pkt)
            if pkt:
                yield pkt
