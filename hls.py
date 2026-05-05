"""
FFmpeg HLS streamer.

Takes raw H.264 NAL units / Annex-B from the camera and feeds them into
an FFmpeg subprocess that produces an HLS playlist + segments on disk.

One FFmpegStreamer per channel.  The main server calls .write(frame_bytes)
whenever a complete video frame arrives from the JT1078 assembler.
"""

import asyncio
import logging
import os
import time
from pathlib import Path

log = logging.getLogger("hls")

# HLS output lives under ./static/hls/<channel>/
HLS_BASE = Path(__file__).parent.parent / "static" / "hls"

# ffmpeg command template.
# -f h264           : input is raw Annex-B H.264
# -i pipe:0         : read from stdin
# -c:v copy         : do NOT re-encode — the camera already sends H.264
# -hls_time         : target segment duration in seconds
# -hls_list_size    : number of segments to keep in the playlist
# -hls_flags        : delete_segments = remove old files automatically
# -start_number     : start segment numbering from 0
FFMPEG_CMD = [
    "ffmpeg",
    "-loglevel", "warning",
    "-f", "h264",
    "-i", "pipe:0",
    "-c:v", "copy",
    "-f", "hls",
    "-hls_time", "2",
    "-hls_list_size", "5",
    "-hls_flags", "delete_segments+append_list",
    "-hls_segment_filename", "",  # filled in per-channel
    "",                           # playlist path — filled in per-channel
]


class FFmpegStreamer:
    """
    Wraps an ffmpeg subprocess.  Feed raw H.264 Annex-B frames via write().
    HLS output lands in static/hls/<channel>/index.m3u8.
    """

    def __init__(self, channel: int):
        self.channel   = channel
        self._proc     = None
        self._out_dir  = HLS_BASE / str(channel)
        self._playlist = self._out_dir / "index.m3u8"
        self._seg_pat  = str(self._out_dir / "seg%05d.ts")
        self._lock     = asyncio.Lock()
        self._started  = False
        self._frames   = 0
        self._last_frame_ts = 0.0

    @property
    def playlist_path(self) -> Path:
        return self._playlist

    @property
    def is_live(self) -> bool:
        """True if we got a frame in the last 10 seconds."""
        return self._started and (time.time() - self._last_frame_ts) < 10

    async def start(self):
        if self._started:
            return
        self._out_dir.mkdir(parents=True, exist_ok=True)

        cmd = list(FFMPEG_CMD)
        cmd[-2] = self._seg_pat
        cmd[-1] = str(self._playlist)

        log.info("Starting ffmpeg for channel %d: %s", self.channel, " ".join(cmd))
        self._proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.DEVNULL,
            stderr=asyncio.subprocess.PIPE,
        )
        self._started = True
        asyncio.ensure_future(self._log_stderr())
        log.info("FFmpeg started for channel %d (pid %d)", self.channel, self._proc.pid)

    async def _log_stderr(self):
        """Drain ffmpeg stderr so the pipe doesn't block."""
        async for line in self._proc.stderr:
            txt = line.decode(errors="replace").rstrip()
            if txt:
                log.warning("[ffmpeg ch%d] %s", self.channel, txt)

    async def write(self, frame: bytes):
        """
        Write one complete H.264 Annex-B frame to ffmpeg stdin.
        Automatically starts ffmpeg on first call.
        """
        if not self._started:
            await self.start()

        if self._proc is None or self._proc.returncode is not None:
            log.warning("FFmpeg ch%d is dead, restarting", self.channel)
            self._started = False
            await self.start()

        try:
            async with self._lock:
                self._proc.stdin.write(frame)
                await self._proc.stdin.drain()
            self._frames += 1
            self._last_frame_ts = time.time()
        except (BrokenPipeError, ConnectionResetError):
            log.warning("FFmpeg ch%d stdin broken", self.channel)
            self._started = False

    async def stop(self):
        if self._proc and self._proc.returncode is None:
            self._proc.stdin.close()
            try:
                await asyncio.wait_for(self._proc.wait(), timeout=5)
            except asyncio.TimeoutError:
                self._proc.kill()
        self._started = False
        log.info("FFmpeg ch%d stopped", self.channel)


class StreamManager:
    """
    Manages one FFmpegStreamer per camera channel.
    Called by the JT1078 connection handler.
    """

    def __init__(self):
        self._streamers: dict[int, FFmpegStreamer] = {}

    def get(self, channel: int) -> FFmpegStreamer:
        if channel not in self._streamers:
            self._streamers[channel] = FFmpegStreamer(channel)
        return self._streamers[channel]

    async def write_frame(self, channel: int, frame: bytes):
        await self.get(channel).write(frame)

    def hls_url(self, channel: int, host: str = "localhost", port: int = 8080) -> str:
        return f"http://{host}:{port}/hls/{channel}/index.m3u8"

    def status(self) -> dict:
        return {
            ch: {"live": s.is_live, "frames": s._frames}
            for ch, s in self._streamers.items()
        }

    async def stop_all(self):
        for s in self._streamers.values():
            await s.stop()
