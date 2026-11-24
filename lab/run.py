"""Run the local HAProxy lab and provide its read-only Runtime API to the TUI."""

import json
import os
from pathlib import Path
import shutil
import socket
import socketserver
import subprocess
import tempfile
import threading


LAB = Path(__file__).resolve().parent
STATE = LAB / ".state"
RUNTIME = ("127.0.0.1", 19490)
SHOW_STAT = b"show stat\n"


class RuntimeHandler(socketserver.StreamRequestHandler):
    def handle(self):
        try:
            self.connection.settimeout(3)
            if self.rfile.readline(64) != SHOW_STAT:
                return
            with socket.create_connection(RUNTIME, timeout=2) as haproxy:
                haproxy.sendall(SHOW_STAT)
                with haproxy.makefile("rb") as stats:
                    shutil.copyfileobj(stats, self.wfile)
        except OSError:
            return


def check_local_docker():
    endpoint = os.environ.get("DOCKER_HOST")
    if os.environ.get("DOCKER_CONTEXT") or not endpoint:
        endpoint = subprocess.check_output(
            ["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"],
            text=True,
        ).strip()
    if not endpoint.startswith("unix://"):
        raise SystemExit("Локальный стенд требует Docker context с локальным Unix-сокетом.")


def main():
    check_local_docker()
    STATE.mkdir(mode=0o700, exist_ok=True)
    config = STATE / "haproxy.cfg"
    if not config.exists():
        shutil.copy(LAB / "haproxy.cfg.example", config)

    subprocess.run(["docker", "compose", "-f", str(LAB / "compose.yaml"), "up", "-d", "--wait"], check=True)
    with tempfile.TemporaryDirectory(prefix="hbm-lab-", dir="/tmp") as session:
        unix_socket = str(Path(session) / "stats.sock")
        profile = Path(session) / "settings.toml"
        profile.write_text(
            f"[general]\nconfig_path = {json.dumps(str(config), ensure_ascii=False)}\n"
            f"backup_dir = {json.dumps(str(STATE / 'backups'), ensure_ascii=False)}\n"
            '\n[haproxy]\nmode = "docker"\n'
            '\n[docker]\ncontainer_name = "haproxy-bridge-lab"\n'
            f"compose_file = {json.dumps(str(LAB / 'compose.yaml'), ensure_ascii=False)}\n"
            'service_name = "haproxy"\napply_method = "recreate"\n'
            '\n[network]\ndefault_bind_address = "0.0.0.0"\ndefault_target_port = 80\n'
            f"\n[monitoring]\nruntime_socket = {json.dumps(unix_socket)}\n",
            encoding="utf-8",
        )
        with socketserver.ThreadingUnixStreamServer(unix_socket, RuntimeHandler) as relay:
            thread = threading.Thread(target=relay.serve_forever, daemon=True)
            thread.start()
            try:
                return subprocess.call(
                    [str(LAB.parent / "bin" / "haproxy-bridge"), "--settings", str(profile)]
                )
            finally:
                relay.shutdown()
                thread.join()


if __name__ == "__main__":
    raise SystemExit(main())
