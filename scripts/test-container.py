#!/usr/bin/env python3
"""Check non-root startup and binding persistence with mock Telegram/Memos APIs."""
import email.parser
import email.policy
import http.server
import json
import re
import subprocess
import sys
import threading
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


class MockAPI(http.server.BaseHTTPRequestHandler):
    updates = []
    replies = []
    condition = threading.Condition()

    def log_message(self, *args):
        pass

    def read_body(self):
        if self.headers.get("Transfer-Encoding") == "chunked":
            body = bytearray()
            while True:
                size = int(self.rfile.readline().split(b";")[0], 16)
                if not size:
                    self.rfile.readline()
                    return bytes(body)
                body.extend(self.rfile.read(size))
                self.rfile.read(2)
        return self.rfile.read(int(self.headers.get("Content-Length", 0)))

    def do_GET(self):
        self.respond(b"")

    def do_POST(self):
        self.respond(self.read_body())

    def respond(self, body):
        if self.path == "/api/instance/profile":
            result = {"version": "container-smoke"}
        elif self.path == "/api/auth/me":
            result = {"user": {"name": "users/test", "displayName": "Test"}}
        elif self.path == "/api/memos":
            result = {"name": "memos/container-smoke", "visibility": "PRIVATE", "state": "NORMAL", "content": json.loads(body)["content"]}
        elif self.path.endswith("/getMe"):
            result = {"ok": True, "result": {"id": 123, "is_bot": True, "first_name": "Test", "username": "memogram_test"}}
        elif self.path.endswith("/getUpdates"):
            with self.condition:
                self.condition.wait(timeout=0.1)
                result = {"ok": True, "result": self.updates[:]}
                self.updates.clear()
        elif self.path.endswith("/setMyCommands"):
            result = {"ok": True, "result": True}
        elif self.path.endswith("/sendMessage"):
            message = email.parser.BytesParser(policy=email.policy.default).parsebytes(
                ("Content-Type: " + self.headers["Content-Type"] + "\r\n\r\n").encode() + body
            )
            text = next(part.get_payload(decode=True).decode() for part in message.iter_parts()
                        if part.get_param("name", header="content-disposition") == "text")
            with self.condition:
                self.replies.append(text)
                self.condition.notify_all()
            result = {"ok": True, "result": {"message_id": 2}}
        else:
            self.send_error(404)
            return
        payload = json.dumps(result).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


def main():
    image = sys.argv[1]
    metadata = docker("run", "--rm", image, "--version")
    if not re.fullmatch(r"memogram [1-9][0-9]\.(0[1-9]|1[0-2])\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*|-dev\.[0-9a-f]{7})? \(commit [0-9a-f]{40}\)", metadata):
        raise RuntimeError("invalid container version metadata: " + metadata)
    if len(sys.argv) > 2 and not metadata.startswith("memogram " + sys.argv[2] + " "):
        raise RuntimeError("unexpected container version: " + metadata)
    if docker("image", "inspect", image, "--format", "{{.Config.User}}") != "65532:65532":
        raise RuntimeError("container must run as UID/GID 65532")
    name = "memogram-smoke-" + uuid.uuid4().hex[:12]
    volume = name + "-data"
    server = http.server.ThreadingHTTPServer(("0.0.0.0", 0), MockAPI)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    base_url = "http://host.docker.internal:" + str(server.server_port)
    docker("volume", "create", volume)
    try:
        for index, text in enumerate(["/start integration-test-token", "capture after container recreation"]):
            with MockAPI.condition:
                MockAPI.replies.clear()
                MockAPI.updates[:] = [{"update_id": index + 1, "message": {
                    "message_id": index + 1, "date": 1, "chat": {"id": 42, "type": "private"},
                    "from": {"id": 42, "is_bot": False, "first_name": "Test"}, "text": text,
                }}]
            docker("run", "-d", "--name", name,
                   "--add-host", "host.docker.internal:host-gateway",
                   "--mount", "type=volume,source=" + volume + ",target=/data",
                   "-e", "SERVER_ADDR=" + base_url, "-e", "BOT_PROXY_ADDR=" + base_url,
                   "-e", "BOT_TOKEN=123:test-token", image)
            expected = "Hello Test!" if index == 0 else "Content saved"
            with MockAPI.condition:
                ok = MockAPI.condition.wait_for(lambda: any(expected in reply for reply in MockAPI.replies), timeout=30)
            if not ok:
                raise RuntimeError("container smoke failed:\n" + docker("logs", name))
            docker("stop", "--time", "5", name)
            docker("rm", name)
        print("Container smoke passed: CalVer metadata, non-root startup, token binding, recreation, saved memo.")
    finally:
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        docker("volume", "rm", volume)
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
