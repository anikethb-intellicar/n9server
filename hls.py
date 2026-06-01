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
HLS_BASE = Path(__file__).parent / "static" / "hls"

# ffmpeg command template.
# -f h264           : input is raw Annex-B H.264
# -i pipe:0         : read from stdin
# -c:v copy         : do NOT re-encode — the camera already sends H.264
# -hls_time         : target segment duration in seconds
# -hls_list_size    : number of segments to keep in the playlist
# -hls_flags        : delete_segments = remove old files automatically
# -start_number     : start segment numbering from 0
def _ffmpeg_cmd(codec: str = "h264") -> list:
    """Build FFmpeg command for the given input codec (h264 or hevc)."""
    fmt = "hevc" if codec in ("H265", "hevc", "h265") else "h264"
    return [
        "ffmpeg",
        "-loglevel", "warning",
        "-fflags", "+genpts+discardcorrupt",
        "-r", "25",
        "-f", fmt,
        "-i", "pipe:0",
        "-c:v", "libx264",         # transcode to H264 for browser compatibility
        "-preset", "ultrafast",
        "-tune", "zerolatency",
        "-crf", "23",
        "-g", "50",
        "-f", "hls",
        "-hls_time", "2",
        "-hls_list_size", "6",
        "-hls_flags", "delete_segments",
        "-hls_segment_filename", "",  # filled in per-channel
        "",                           # playlist path — filled in per-channel
    ]


class FFmpegStreamer:
    """
    Wraps an ffmpeg subprocess.  Feed raw Annex-B frames via write().
    HLS output lands in static/hls/<channel>/index.m3u8.
    """

    _VPS_PREFIX   = b'\x00\x00\x00\x01\x40'   # H265 VPS NAL
    _START_CODE   = b'\x00\x00\x00\x01'
    # RASL NAL types cause gray frames — they reference pre-keyframe frames
    # RASL_R=type8 (hdr=0x10), RASL_N=type9 (hdr=0x12)
    _RASL_HDRS    = {0x10, 0x12}

    def __init__(self, channel: int, codec: str = "h264"):
        self.channel    = channel
        self._codec     = codec
        self._proc      = None
        self._out_dir   = HLS_BASE / str(channel)
        self._playlist  = self._out_dir / "index.m3u8"
        self._seg_pat   = str(self._out_dir / "seg%05d.ts")
        self._lock      = asyncio.Lock()
        self._started   = False
        self._frames    = 0
        self._last_frame_ts = 0.0
        self._pre_buf   = bytearray()   # buffer until first keyframe
        self._got_keyframe = False

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

        cmd = _ffmpeg_cmd(self._codec)
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

    def _strip_rasl(self, data: bytes) -> bytes:
        """Remove H265 RASL NAL units from Annex-B stream to eliminate gray frames."""
        result = bytearray()
        pos = 0
        while pos < len(data):
            idx = data.find(self._START_CODE, pos)
            if idx == -1:
                result.extend(data[pos:])
                break
            result.extend(data[pos:idx])
            next_idx = data.find(self._START_CODE, idx + 4)
            nal_end = next_idx if next_idx != -1 else len(data)
            # Check NAL type header byte (5th byte = first byte of NAL header)
            if idx + 4 < len(data) and data[idx + 4] in self._RASL_HDRS:
                pos = nal_end  # skip RASL NAL unit
                continue
            result.extend(data[idx:nal_end])
            pos = nal_end
        return bytes(result)

    async def write(self, frame: bytes):
        """Feed payload bytes to ffmpeg, waiting for IDR before starting."""
        self._frames += 1
        self._last_frame_ts = time.time()

        if not self._got_keyframe:
            self._pre_buf.extend(frame)
            buf = bytes(self._pre_buf)
            vps_idx = buf.find(self._VPS_PREFIX)
            if vps_idx == -1:
                if len(self._pre_buf) > 100_000:
                    self._pre_buf.clear()
                return
            frame = buf[vps_idx:]
            self._pre_buf.clear()
            self._got_keyframe = True
            log.info("VPS found for ch%d at buf[%d] — starting FFmpeg", self.channel, vps_idx)
            await self.start()

        if not self._started:
            await self.start()

        if self._proc is None or self._proc.returncode is not None:
            log.warning("FFmpeg ch%d is dead, restarting on next keyframe", self.channel)
            self._started = False
            self._got_keyframe = False
            return

        try:
            async with self._lock:
                self._proc.stdin.write(frame)
                await self._proc.stdin.drain()
        except (BrokenPipeError, ConnectionResetError):
            log.warning("FFmpeg ch%d stdin broken", self.channel)
            self._started = False
            self._got_keyframe = False

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

    def get(self, channel: int, codec: str = "h264") -> FFmpegStreamer:
        if channel not in self._streamers:
            self._streamers[channel] = FFmpegStreamer(channel, codec=codec)
        return self._streamers[channel]

    async def write_frame(self, channel: int, frame: bytes, codec: str = "h264"):
        await self.get(channel, codec=codec).write(frame)

    async def reset(self, channel: int):
        """Stop and discard the existing streamer for this channel (fresh JT1078 connection)."""
        if channel in self._streamers:
            log.info("Resetting streamer for channel %d (new JT1078 connection)", channel)
            await self._streamers[channel].stop()
            del self._streamers[channel]

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
