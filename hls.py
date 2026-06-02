"""
MJPEG live streamer — H265 → JPEG frames → HTTP multipart stream.
GOP-burst smoothing: frames are queued and paced to HTTP clients at target fps.
"""

import asyncio
import logging
import time

log = logging.getLogger("streamer")

_TARGET_FPS    = 25
_FRAME_BUF     = 30           # hold up to one full GOP worth of JPEG frames
_MAX_WRITE_BUF = 512 * 1024   # drop HTTP clients whose send buffer exceeds this

# H265 NAL unit types 8/9 are RASL (Random Access Skipped Leading).
# These reference frames that predate the random access point and decode gray.
_H265_RASL_N = 8
_H265_RASL_R = 9

# Annex-B prefixes for H265 keyframe NALs (nuh_layer_id=0, nuh_temporal_id=1)
_H265_IDR_PREFIXES = (
    b'\x00\x00\x00\x01\x26',  # IDR_W_RADL  nal_type 19
    b'\x00\x00\x00\x01\x28',  # IDR_N_LP    nal_type 20
    b'\x00\x00\x00\x01\x2a',  # CRA_NUT     nal_type 21
)


def _is_h265_rasl(frame: bytes) -> bool:
    """Return True only when frame contains RASL NAL(s) and no non-RASL picture NAL.

    Standalone RASL packets are dropped. Combined keyframe packets that happen to
    contain RASL alongside an IDR/CRA are kept so FFmpeg receives the full sequence.
    """
    has_rasl = False
    i = 0
    n = len(frame)
    while i < n - 4:
        if frame[i:i+4] == b'\x00\x00\x00\x01':
            if i + 5 > n:
                break
            nal_type = (frame[i + 4] >> 1) & 0x3F
            i += 4
        elif frame[i:i+3] == b'\x00\x00\x01':
            if i + 4 > n:
                break
            nal_type = (frame[i + 3] >> 1) & 0x3F
            i += 3
        else:
            i += 1
            continue
        if nal_type in (_H265_RASL_N, _H265_RASL_R):
            has_rasl = True
        elif nal_type < 32:
            # Non-RASL picture NAL present — keep entire frame
            return False
        # nal_type >= 32: parameter set or SEI, keep scanning
    return has_rasl


def _ffmpeg_cmd(codec: str = "h265") -> list:
    fmt = "hevc" if codec in ("H265", "hevc", "h265") else "h264"
    return [
        "ffmpeg",
        "-loglevel", "warning",
        "-fflags", "+genpts+discardcorrupt",
        "-r", str(_TARGET_FPS),
        "-f", fmt,
        "-i", "pipe:0",
        "-vf", "scale=640:360",
        "-c:v", "mjpeg",
        "-q:v", "4",
        "-flush_packets", "1",
        "-f", "mjpeg",
        "pipe:1",
    ]


class LiveStreamer:
    """
    H265 → MJPEG streamer with GOP-burst smoothing.
    Incoming H265 bursts are queued and paced out at _TARGET_FPS to HTTP clients.
    Non-blocking writes: slow clients are dropped when their buffer exceeds _MAX_WRITE_BUF.
    """

    _VPS_PREFIX   = b'\x00\x00\x00\x01\x40'
    _JPEG_SOI     = b'\xff\xd8'
    _JPEG_EOI     = b'\xff\xd9'
    _BOUNDARY     = b'--frame\r\nContent-Type: image/jpeg\r\n\r\n'
    _BOUNDARY_END = b'\r\n'

    def __init__(self, channel: int, codec: str = "h265"):
        self.channel        = channel
        self._codec         = codec
        self._proc          = None
        self._started       = False
        self._got_keyframe  = False
        self._pre_buf       = bytearray()
        self._frames        = 0
        self._last_frame_ts = 0.0
        self._http_clients: set = set()
        self._frame_queue: asyncio.Queue = asyncio.Queue(maxsize=_FRAME_BUF)

    @property
    def is_live(self) -> bool:
        return self._started and (time.time() - self._last_frame_ts) < 10

    def add_http_client(self, writer):
        self._http_clients.add(writer)
        log.info("MJPEG client added ch%d (%d total)", self.channel, len(self._http_clients))

    def remove_http_client(self, writer):
        self._http_clients.discard(writer)
        log.info("MJPEG client removed ch%d (%d left)", self.channel, len(self._http_clients))

    async def start(self):
        if self._started:
            return
        self._frame_queue = asyncio.Queue(maxsize=_FRAME_BUF)
        cmd = _ffmpeg_cmd(self._codec)
        log.info("Starting MJPEG streamer for channel %d", self.channel)
        self._proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        self._started = True
        asyncio.ensure_future(self._read_jpeg_frames())
        asyncio.ensure_future(self._pace_frames())
        asyncio.ensure_future(self._log_stderr())

    async def _read_jpeg_frames(self):
        """Read MJPEG output from FFmpeg and enqueue each complete frame."""
        buf = bytearray()
        try:
            while self._proc and self._proc.returncode is None:
                chunk = await self._proc.stdout.read(65536)
                if not chunk:
                    break
                buf.extend(chunk)
                while True:
                    start = buf.find(self._JPEG_SOI)
                    if start == -1:
                        buf.clear()
                        break
                    end = buf.find(self._JPEG_EOI, start + 2)
                    if end == -1:
                        if start > 0:
                            del buf[:start]
                        break
                    jpeg = bytes(buf[start:end + 2])
                    del buf[:end + 2]
                    if self._http_clients:
                        # Drop oldest frame if full — live stream favours freshness
                        if self._frame_queue.full():
                            try:
                                self._frame_queue.get_nowait()
                            except asyncio.QueueEmpty:
                                pass
                        try:
                            self._frame_queue.put_nowait(jpeg)
                        except asyncio.QueueFull:
                            pass
        except Exception as e:
            log.debug("MJPEG read error ch%d: %s", self.channel, e)
        finally:
            self._started = False

    async def _pace_frames(self):
        """Deliver queued JPEG frames to HTTP clients at _TARGET_FPS.

        GOP bursts fill the queue quickly; this coroutine drains it at a steady
        40 ms cadence so the browser sees smooth motion instead of frame floods
        followed by silence.
        """
        interval = 1.0 / _TARGET_FPS
        last_push = time.monotonic() - interval
        try:
            while True:
                try:
                    jpeg = await asyncio.wait_for(self._frame_queue.get(), timeout=2.0)
                except asyncio.TimeoutError:
                    if not self._started:
                        break
                    continue
                now = time.monotonic()
                wait = interval - (now - last_push)
                if wait > 0.001:
                    await asyncio.sleep(wait)
                last_push = time.monotonic()
                if self._http_clients:
                    self._push_frame_nowait(jpeg)
        except asyncio.CancelledError:
            pass
        except Exception as e:
            log.debug("Pacer ch%d: %s", self.channel, e)

    def _push_frame_nowait(self, jpeg: bytes):
        """Write one JPEG frame to all connected clients (non-blocking).

        Avoids awaiting drain per-frame so a slow client cannot stall delivery
        to other clients. Clients whose send buffer exceeds _MAX_WRITE_BUF are dropped.
        """
        data = self._BOUNDARY + jpeg + self._BOUNDARY_END
        dead = set()
        for writer in list(self._http_clients):
            try:
                writer.write(data)
                if writer.transport and writer.transport.get_write_buffer_size() > _MAX_WRITE_BUF:
                    dead.add(writer)
            except Exception:
                dead.add(writer)
        for w in dead:
            self._http_clients.discard(w)
            try:
                w.close()
            except Exception:
                pass

    async def _log_stderr(self):
        async for line in self._proc.stderr:
            txt = line.decode(errors="replace").rstrip()
            if txt:
                log.warning("[mjpeg ch%d] %s", self.channel, txt)

    async def write(self, frame: bytes):
        """Feed raw H265 payload to FFmpeg. Buffers until first clean keyframe."""
        self._frames += 1
        self._last_frame_ts = time.time()

        # Drop standalone H265 RASL frames — they reference pre-CRA data and go gray
        if self._codec in ("H265", "hevc", "h265") and _is_h265_rasl(frame):
            return

        if not self._got_keyframe:
            self._pre_buf.extend(frame)
            buf = bytes(self._pre_buf)

            start_idx = -1
            vps_idx = buf.find(self._VPS_PREFIX)
            if vps_idx != -1:
                start_idx = vps_idx
                log.info("VPS found ch%d — starting MJPEG stream", self.channel)
            else:
                # No VPS yet — check for IDR/CRA (some cameras omit VPS after first GOP)
                for prefix in _H265_IDR_PREFIXES:
                    idx = buf.find(prefix)
                    if idx != -1 and (start_idx == -1 or idx < start_idx):
                        start_idx = idx
                if start_idx != -1:
                    log.info("IDR/CRA found ch%d (no VPS) — starting stream", self.channel)
                elif len(self._pre_buf) >= 50_000:
                    # Last resort: dump is clearly wrong, start anyway so stream isn't stuck
                    start_idx = 0
                    log.warning("No keyframe ch%d after 50KB — forcing start", self.channel)
                else:
                    return

            frame = buf[start_idx:]
            self._pre_buf.clear()
            self._got_keyframe = True
            await self.start()

        if not self._started:
            await self.start()

        if self._proc is None or self._proc.returncode is not None:
            self._started = False
            self._got_keyframe = False
            return

        try:
            self._proc.stdin.write(frame)
            await self._proc.stdin.drain()
        except (BrokenPipeError, ConnectionResetError):
            log.warning("MJPEG ch%d stdin broken", self.channel)
            self._started = False
            self._got_keyframe = False

    async def stop(self):
        self._got_keyframe = False
        self._pre_buf.clear()
        if self._proc and self._proc.returncode is None:
            self._proc.stdin.close()
            try:
                await asyncio.wait_for(self._proc.wait(), timeout=5)
            except asyncio.TimeoutError:
                self._proc.kill()
        self._started = False
        log.info("MJPEG ch%d stopped", self.channel)


class StreamManager:
    """Manages one LiveStreamer per camera channel."""

    def __init__(self):
        self._streamers: dict[int, LiveStreamer] = {}

    @property
    def status(self):
        return {
            str(ch): {
                "live": s.is_live,
                "frames": s._frames,
                "clients": len(s._http_clients),
            }
            for ch, s in self._streamers.items()
        }

    def get(self, channel: int, codec: str = "h265") -> LiveStreamer:
        if channel not in self._streamers:
            self._streamers[channel] = LiveStreamer(channel, codec=codec)
        return self._streamers[channel]

    async def write_frame(self, channel: int, frame: bytes, codec: str = "h265"):
        await self.get(channel, codec=codec).write(frame)

    async def soft_reset(self, channel: int):
        if channel in self._streamers:
            s = self._streamers[channel]
            if s._started:
                s._got_keyframe = False
                s._pre_buf.clear()
                log.info("Soft-reset ch%d (FFmpeg continues, waiting for next keyframe)", channel)
                return
        await self.reset(channel)

    async def reset(self, channel: int):
        if channel in self._streamers:
            await self._streamers[channel].stop()
            del self._streamers[channel]

    def add_mjpeg_client(self, channel: int, writer):
        self.get(channel).add_http_client(writer)

    def remove_mjpeg_client(self, channel: int, writer):
        if channel in self._streamers:
            self._streamers[channel].remove_http_client(writer)

    async def stop_all(self):
        for s in self._streamers.values():
            await s.stop()
