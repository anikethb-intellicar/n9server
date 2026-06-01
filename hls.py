"""
MJPEG live streamer — H265 → JPEG frames → HTTP multipart stream.
True real-time: each frame sent immediately, no buffering, no storage.
"""

import asyncio
import logging
import time
from pathlib import Path

log = logging.getLogger("streamer")


def _ffmpeg_cmd(codec: str = "h265") -> list:
    fmt = "hevc" if codec in ("H265", "hevc", "h265") else "h264"
    return [
        "ffmpeg",
        "-loglevel", "warning",
        "-fflags", "+genpts+discardcorrupt",
        "-r", "25",
        "-f", fmt,
        "-i", "pipe:0",
        "-vf", "scale=640:360",   # smaller resolution for fast JPEG encoding
        "-c:v", "mjpeg",          # JPEG output — no inter-frame prediction needed
        "-q:v", "4",              # quality 1-31 (lower = better)
        "-f", "mjpeg",            # raw MJPEG output stream
        "pipe:1",
    ]


class LiveStreamer:
    """
    H265 → MJPEG streamer. Pushes JPEG frames to connected HTTP clients in real-time.
    No segments, no storage, no MSE complexity.
    """

    _VPS_PREFIX = b'\x00\x00\x00\x01\x40'
    _JPEG_SOI   = b'\xff\xd8'   # JPEG Start of Image
    _JPEG_EOI   = b'\xff\xd9'   # JPEG End of Image
    _BOUNDARY   = b'--frame\r\nContent-Type: image/jpeg\r\n\r\n'
    _BOUNDARY_END = b'\r\n'

    def __init__(self, channel: int, codec: str = "h265"):
        self.channel      = channel
        self._codec       = codec
        self._proc        = None
        self._started     = False
        self._got_keyframe = False
        self._pre_buf     = bytearray()
        self._frames      = 0
        self._last_frame_ts = 0.0
        self._http_clients: set = set()   # asyncio.StreamWriter for each MJPEG client

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
        asyncio.ensure_future(self._log_stderr())

    async def _read_jpeg_frames(self):
        """Read MJPEG output from FFmpeg and push each frame to clients."""
        buf = bytearray()
        try:
            while self._proc and self._proc.returncode is None:
                chunk = await self._proc.stdout.read(65536)
                if not chunk:
                    break
                buf.extend(chunk)

                # Extract complete JPEG frames from buffer
                while True:
                    start = buf.find(self._JPEG_SOI)
                    if start == -1:
                        buf.clear()
                        break
                    end = buf.find(self._JPEG_EOI, start + 2)
                    if end == -1:
                        # Incomplete frame — keep from start
                        if start > 0:
                            del buf[:start]
                        break
                    # Complete JPEG frame found
                    jpeg = bytes(buf[start:end + 2])
                    del buf[:end + 2]

                    if self._http_clients:
                        await self._push_frame(jpeg)

        except Exception as e:
            log.debug("MJPEG read error ch%d: %s", self.channel, e)
        finally:
            self._started = False

    async def _push_frame(self, jpeg: bytes):
        """Push one JPEG frame to all connected clients."""
        data = self._BOUNDARY + jpeg + self._BOUNDARY_END
        dead = set()
        for writer in list(self._http_clients):
            try:
                writer.write(data)
                await writer.drain()
            except Exception:
                dead.add(writer)
        self._http_clients -= dead

    async def _log_stderr(self):
        async for line in self._proc.stderr:
            txt = line.decode(errors="replace").rstrip()
            if txt:
                log.warning("[mjpeg ch%d] %s", self.channel, txt)

    async def write(self, frame: bytes):
        """Feed raw H265 payload — buffer until VPS keyframe, then start FFmpeg."""
        self._frames += 1
        self._last_frame_ts = time.time()

        if not self._got_keyframe:
            self._pre_buf.extend(frame)
            buf = bytes(self._pre_buf)
            vps_idx = buf.find(self._VPS_PREFIX)
            if vps_idx != -1:
                frame = buf[vps_idx:]
                log.info("VPS found ch%d — starting MJPEG stream", self.channel)
            elif len(self._pre_buf) >= 5_000:
                frame = buf
                log.info("No VPS ch%d after 5KB — starting anyway", self.channel)
            else:
                return
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
                log.info("Soft-reset ch%d (MJPEG streamer continues)", channel)
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
