"""
rtsp_relay.py — MJPEG → RTSP relay for N9 dashcam server
==========================================================

Polls the N9 server's /status API, and for every live channel
spawns an FFmpeg process that:

  pulls  →  http://<server>/mjpeg/<phone>/<channel>   (MJPEG)
  pushes →  rtsp://localhost:8554/<phone>/<channel>   (H264 RTSP via mediamtx)

Requirements
------------
1. mediamtx (single binary, no deps):
       wget -q https://github.com/bluenviron/mediamtx/releases/latest/download/mediamtx_linux_amd64.tar.gz
       tar -xf mediamtx_linux_amd64.tar.gz
       ./mediamtx &          # runs on :8554 by default

2. FFmpeg with libx264:
       sudo apt-get install -y ffmpeg

Usage
-----
    python3 rtsp_relay.py
    python3 rtsp_relay.py --server http://192.168.1.100:8080
    python3 rtsp_relay.py --server http://localhost:8080 --rtsp-host 0.0.0.0 --rtsp-port 8554

RTSP URLs available after relay starts:
    rtsp://<server_ip>:8554/<phone>/<channel>
    e.g. rtsp://10.0.0.5:8554/088556000002/1
"""

import argparse
import asyncio
import json
import logging
import signal
import sys
import time
import urllib.request
from dataclasses import dataclass, field

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)-7s %(name)-12s %(message)s",
    datefmt="%H:%M:%S",
)
log = logging.getLogger("rtsp_relay")


# ── config ────────────────────────────────────────────────────────────────────

@dataclass
class Config:
    server:    str = "http://localhost:8080"
    rtsp_host: str = "localhost"
    rtsp_port: int = 8554
    poll_interval: float = 5.0      # seconds between /status polls
    idle_timeout:  float = 15.0     # kill relay if channel not live for this long
    fps:           int   = 25


# ── per-channel relay ─────────────────────────────────────────────────────────

@dataclass
class Relay:
    phone:    str
    channel:  int
    proc:     asyncio.subprocess.Process = field(default=None, repr=False)
    last_live: float = field(default_factory=time.monotonic)

    @property
    def key(self) -> tuple:
        return (self.phone, self.channel)


def _build_ffmpeg_cmd(mjpeg_url: str, rtsp_url: str, fps: int) -> list[str]:
    """
    Pull MJPEG from the N9 server and push H264 RTSP to mediamtx.

    -re         read input at native frame rate (prevents flooding)
    ultrafast   lowest CPU cost for live encoding
    zerolatency minimise encode latency
    """
    return [
        "ffmpeg",
        "-loglevel", "warning",
        "-re",
        "-fflags", "+discardcorrupt",
        "-i", mjpeg_url,
        "-an",                           # no audio
        "-c:v", "libx264",
        "-preset", "ultrafast",
        "-tune", "zerolatency",
        "-r", str(fps),
        "-g", str(fps * 2),              # keyframe every 2 s
        "-f", "rtsp",
        "-rtsp_transport", "tcp",
        rtsp_url,
    ]


# ── relay manager ─────────────────────────────────────────────────────────────

class RelayManager:
    def __init__(self, cfg: Config):
        self._cfg     = cfg
        self._relays: dict[tuple, Relay] = {}
        self._running = True

    def _mjpeg_url(self, phone: str, channel: int) -> str:
        return f"{self._cfg.server}/mjpeg/{phone}/{channel}"

    def _rtsp_url(self, phone: str, channel: int) -> str:
        return f"rtsp://{self._cfg.rtsp_host}:{self._cfg.rtsp_port}/{phone}/{channel}"

    async def _start_relay(self, phone: str, channel: int) -> Relay:
        mjpeg = self._mjpeg_url(phone, channel)
        rtsp  = self._rtsp_url(phone, channel)
        cmd   = _build_ffmpeg_cmd(mjpeg, rtsp, self._cfg.fps)

        log.info("Starting relay  %s/ch%d  →  %s", phone[-8:], channel, rtsp)
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdin=asyncio.subprocess.DEVNULL,
            stdout=asyncio.subprocess.DEVNULL,
            stderr=asyncio.subprocess.PIPE,
        )
        relay = Relay(phone=phone, channel=channel, proc=proc)
        asyncio.ensure_future(self._monitor_stderr(relay))
        return relay

    async def _monitor_stderr(self, relay: Relay):
        if relay.proc is None:
            return
        async for line in relay.proc.stderr:
            txt = line.decode(errors="replace").rstrip()
            if txt:
                log.debug("[ffmpeg %s/ch%d] %s", relay.phone[-8:], relay.channel, txt)

    async def _stop_relay(self, relay: Relay):
        log.info("Stopping relay  %s/ch%d", relay.phone[-8:], relay.channel)
        if relay.proc and relay.proc.returncode is None:
            relay.proc.terminate()
            try:
                await asyncio.wait_for(relay.proc.wait(), timeout=5)
            except asyncio.TimeoutError:
                relay.proc.kill()

    async def _fetch_status(self) -> dict:
        url = f"{self._cfg.server}/status"
        loop = asyncio.get_event_loop()
        try:
            raw = await loop.run_in_executor(
                None,
                lambda: urllib.request.urlopen(url, timeout=5).read()
            )
            return json.loads(raw)
        except Exception as e:
            log.warning("Failed to fetch %s: %s", url, e)
            return {}

    async def _reconcile(self, status: dict):
        """Start/stop relays to match the server's live channels."""
        now = time.monotonic()
        desired: set[tuple] = set()

        for phone, info in status.get("devices", {}).items():
            for ch_str, stream in info.get("streams", {}).items():
                if stream.get("live"):
                    key = (phone, int(ch_str))
                    desired.add(key)
                    if key in self._relays:
                        self._relays[key].last_live = now

        # Start missing relays
        for (phone, ch) in desired:
            if (phone, ch) not in self._relays:
                relay = await self._start_relay(phone, ch)
                relay.last_live = now
                self._relays[(phone, ch)] = relay

        # Restart crashed relays that are still desired
        for key, relay in list(self._relays.items()):
            if key in desired and relay.proc and relay.proc.returncode is not None:
                log.warning("Relay %s/ch%d crashed (rc=%d) — restarting",
                            relay.phone[-8:], relay.channel, relay.proc.returncode)
                await self._stop_relay(relay)
                new_relay = await self._start_relay(*key)
                new_relay.last_live = now
                self._relays[key] = new_relay

        # Stop stale relays (channel went offline or not seen recently)
        for key, relay in list(self._relays.items()):
            idle = now - relay.last_live
            if key not in desired and idle > self._cfg.idle_timeout:
                await self._stop_relay(relay)
                del self._relays[key]

    async def run(self):
        log.info("Relay manager started — polling %s every %.0fs",
                 self._cfg.server, self._cfg.poll_interval)
        log.info("RTSP base URL: rtsp://%s:%d/<phone>/<channel>",
                 self._cfg.rtsp_host, self._cfg.rtsp_port)
        log.info("Ensure mediamtx is running:  ./mediamtx &")

        while self._running:
            status = await self._fetch_status()
            if status:
                await self._reconcile(status)
                self._log_active()
            await asyncio.sleep(self._cfg.poll_interval)

        # Clean shutdown
        for relay in list(self._relays.values()):
            await self._stop_relay(relay)
        self._relays.clear()

    def _log_active(self):
        if not self._relays:
            return
        for (phone, ch) in sorted(self._relays):
            rtsp = self._rtsp_url(phone, ch)
            log.info("  LIVE  rtsp://%s:%d/%s/%d",
                     self._cfg.rtsp_host, self._cfg.rtsp_port, phone, ch)

    def stop(self):
        self._running = False


# ── entry point ───────────────────────────────────────────────────────────────

async def _main(cfg: Config):
    manager = RelayManager(cfg)

    loop = asyncio.get_event_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, manager.stop)

    await manager.run()


def main():
    p = argparse.ArgumentParser(description="N9 MJPEG → RTSP relay via mediamtx")
    p.add_argument("--server",    default="http://localhost:8080",
                   help="N9 server base URL (default: http://localhost:8080)")
    p.add_argument("--rtsp-host", default="localhost",
                   help="mediamtx host (default: localhost; use 0.0.0.0 to bind all)")
    p.add_argument("--rtsp-port", type=int, default=8554,
                   help="mediamtx RTSP port (default: 8554)")
    p.add_argument("--poll",      type=float, default=5.0,
                   help="seconds between /status polls (default: 5)")
    p.add_argument("--fps",       type=int,   default=25,
                   help="target output fps (default: 25)")
    args = p.parse_args()

    cfg = Config(
        server       = args.server.rstrip("/"),
        rtsp_host    = args.rtsp_host,
        rtsp_port    = args.rtsp_port,
        poll_interval = args.poll,
        fps          = args.fps,
    )

    try:
        asyncio.run(_main(cfg))
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
