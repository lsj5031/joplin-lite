#!/usr/bin/env python3
"""CLI user-story suite for joplin-lite.

Runs the `gojoplin` binary end-to-end for the CLI-* stories and writes
qa/results-cli.json.

    python3 qa/test_cli.py
    python3 qa/test_cli.py CLI-08 CLI-09
"""
import json
import os
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
RUN = Path(__file__).resolve().parent / "run"
BIN = RUN / "gojoplin"
RESULTS = Path(__file__).resolve().parent / "results-cli.json"

SERVER = "http://127.0.0.1:41890"
TOKEN = "api-token"
only = set(sys.argv[1:])
results = {}


def run(args, env=None, timeout=60):
    e = {**os.environ, **(env or {})}
    return subprocess.run([str(BIN), *args], capture_output=True, text=True, cwd=ROOT, env=e, timeout=timeout)


def clip(args, *extra):
    return run(["clip", "--server", SERVER, "--api-key", TOKEN, *args, *extra])


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    p = s.getsockname()[1]
    s.close()
    return p


def wait_http(url, tries=80):
    for _ in range(tries):
        try:
            with urllib.request.urlopen(url, timeout=1) as r:
                return r.status, r.read()
        except Exception:  # noqa: BLE001
            time.sleep(0.25)
    return None, None


def write_cfg(path, **over):
    port = over.pop("port", free_port())
    data_dir = over.pop("data_dir", tempfile.mkdtemp(prefix="gojoplin-cli-"))
    body = {
        "sync": {"target": 9, "joplin_server": {"url": "https://sync.example/base"}},
        "api": {"token": "cli-token", "key": "cli-key", **over.pop("api", {})},
        "server": {"data_dir": data_dir, "listen_host": "127.0.0.1", "port": port},
    }
    body.update(over)
    text = _yaml(body)
    Path(path).write_text(text)
    return port, data_dir


def _yaml(obj, indent=0):
    out = []
    for k, v in obj.items():
        pad = "  " * indent
        if isinstance(v, dict):
            out.append(f"{pad}{k}:")
            out.append(_yaml(v, indent + 1))
        else:
            out.append(f"{pad}{k}: {json.dumps(v)}")
    return "\n".join(out) + "\n"


def main():
    tests = []

    def test(story_id, name):
        def deco(fn):
            tests.append((story_id, name, fn))
            return fn
        return deco

    tmp = Path(tempfile.mkdtemp(prefix="gojoplin-cli-tests-"))

    @test("CLI-01", "help lists commands")
    def _():
        r = run(["--help"])
        assert r.returncode == 0, r.stderr
        for cmd in ("serve", "sync", "config", "dropbox-auth", "clip"):
            assert cmd in r.stdout, f"{cmd} missing from help"

    @test("CLI-02", "config prints resolved settings")
    def _():
        cfg = tmp / "print.yaml"
        write_cfg(cfg)
        r = run(["--config", str(cfg), "config"], env={"GOJOPLIN_USERNAME": "qa"})
        assert r.returncode == 0, r.stderr
        for needle in ("Sync Target", "Server URL", "Username", "Data Dir", "Listen Address", "Tracing"):
            assert needle in r.stdout, f"{needle} missing from config:\n{r.stdout}"
        assert "qa" in r.stdout, "username missing"
        assert "cli-token" not in r.stdout and "cli-key" not in r.stdout, "secrets must be redacted"

    @test("CLI-03", "serve requires an api token")
    def _():
        cfg = tmp / "notoken.yaml"
        write_cfg(cfg, api={"token": "", "key": "cli-key"})
        r = run(["--config", str(cfg), "serve", "--no-sync"])
        assert r.returncode != 0, "expected failure"
        assert "api token is required" in (r.stderr + r.stdout).lower(), r.stderr

    @test("CLI-04", "serve requires an api key")
    def _():
        cfg = tmp / "nokey.yaml"
        write_cfg(cfg, api={"token": "cli-token", "key": ""})
        r = run(["--config", str(cfg), "serve", "--no-sync"])
        assert r.returncode != 0, "expected failure"
        assert "api key is required" in (r.stderr + r.stdout).lower(), r.stderr

    @test("CLI-05", "serve --no-sync is local only")
    def _():
        cfg = tmp / "local.yaml"
        port, _ = write_cfg(cfg)
        proc = subprocess.Popen([str(BIN), "--config", str(cfg), "serve", "--no-sync"],
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, cwd=ROOT)
        try:
            status, body = wait_http(f"http://127.0.0.1:{port}/ping")
            assert status == 200 and body == b"JoplinClipperServer", (status, body)
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()

    @test("CLI-06", "serve announces its address")
    def _():
        cfg = tmp / "listen.yaml"
        port, _ = write_cfg(cfg)
        proc = subprocess.Popen([str(BIN), "--config", str(cfg), "serve", "--no-sync"],
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, cwd=ROOT)
        try:
            ok, _ = wait_http(f"http://127.0.0.1:{port}/ping")
            assert ok == 200
        finally:
            proc.terminate()
            out, _ = proc.communicate(timeout=10)
        assert str(port) in out, f"port not announced:\n{out}"

    @test("CLI-07", "one-shot sync fails loudly")
    def _():
        cfg = tmp / "sync.yaml"
        write_cfg(cfg, sync={"target": 7})
        r = run(["--config", str(cfg), "sync"])
        assert r.returncode != 0, "sync without a server must fail"

    @test("CLI-08", "clip ping")
    def _():
        r = clip(["ping"])
        assert r.returncode == 0, r.stderr
        assert "JoplinClipperServer" in r.stdout, r.stdout

    @test("CLI-09", "clip notes round-trip")
    def _():
        r = clip(["notes", "create", "--title", "CLI-09 note", "--body", "hello from cli"])
        assert r.returncode == 0, r.stderr
        nid = _last_id(r.stdout)
        assert nid, r.stdout
        g = clip(["notes", "get", nid])
        assert "CLI-09 note" in g.stdout, g.stdout
        l = clip(["notes", "list", "--limit", "5"])
        assert l.returncode == 0, l.stderr
        d = clip(["notes", "delete", nid])
        assert d.returncode == 0, d.stderr

    @test("CLI-10", "clip folders")
    def _():
        l = clip(["folders", "list"])
        assert l.returncode == 0, l.stderr
        c = clip(["folders", "create", "--title", "CLI-10 folder"])
        assert c.returncode == 0, c.stderr
        fid = _last_id(c.stdout)
        if fid:
            clip(["folders", "delete", fid])

    @test("CLI-11", "clip tags")
    def _():
        l = clip(["tags", "list"])
        assert l.returncode == 0, l.stderr

    @test("CLI-12", "clip search")
    def _():
        clip(["notes", "create", "--title", "CLI-12 searchable", "--body", "find me kumquat"])
        r = clip(["search", "kumquat"])
        assert r.returncode == 0, r.stderr
        assert "CLI-12 searchable" in r.stdout, r.stdout

    @test("CLI-18", "no-sync skips sync validation")
    def _():
        cfg = tmp / "nosync.yaml"
        port = free_port()
        # A config with no sync target or credentials at all must still serve.
        Path(cfg).write_text(_yaml({
            "api": {"token": "cli-token", "key": "cli-key"},
            "server": {"data_dir": tempfile.mkdtemp(prefix="gojoplin-cli-"),
                       "listen_host": "127.0.0.1", "port": port},
        }))
        proc = subprocess.Popen([str(BIN), "--config", str(cfg), "serve", "--no-sync"],
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, cwd=ROOT)
        try:
            status, body = wait_http(f"http://127.0.0.1:{port}/ping")
            assert status == 200 and body == b"JoplinClipperServer", (status, body)
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()

    @test("CLI-19", "data dir env fallback")
    def _():
        cfg = tmp / "nodata.yaml"
        Path(cfg).write_text(_yaml({
            "sync": {"target": 9, "joplin_server": {"url": "https://sync.example/base"}},
            "api": {"token": "cli-token", "key": "cli-key"},
            "server": {"port": free_port()},
        }))
        r = run(["--config", str(cfg), "config"], env={"GOJOPLIN_DATA_DIR": str(tmp / "from-env")})
        assert r.returncode == 0, r.stderr
        assert "from-env" in r.stdout, r.stdout

    @test("CLI-22", "completion scripts")
    def _():
        for shell in ("bash", "zsh", "fish", "powershell"):
            r = run(["completion", shell])
            assert r.returncode == 0, f"{shell}: {r.stderr}"
            assert "gojoplin" in r.stdout, f"{shell} script does not mention gojoplin"
            assert len(r.stdout) > 200, f"{shell} script looks empty"

    @test("OBS-05", "example config matches reality")
    def _():
        text = (ROOT / "config.yaml.example").read_text()
        # Not-yet-implemented subsystems must be off and clearly annotated, so a
        # user copying the example cannot enable a silent no-op.
        for name in ("tracing", "metrics", "rag"):
            assert f"NOT YET IMPLEMENTED" in text, "example must flag unimplemented features"
        assert "13 tools" not in text, "example must not advertise unbuilt MCP tool counts"
        assert "enabled: false" in text
        # The MCP allow-list (which IS implemented) stays documented.
        assert "allow_folders" in text and "allow_tags" in text

    for story_id, name, fn in tests:
        if only and story_id not in only:
            continue
        try:
            fn()
            results[story_id] = {"ok": True, "name": name, "status": "pass", "layer": "cli",
                                 "evidence": f"qa/test_cli.py ({name})"}
            print(f"ok   {story_id} {name}")
        except Exception as e:  # noqa: BLE001
            results[story_id] = {"ok": False, "name": name, "status": "fail", "layer": "cli",
                                 "severity": "medium",
                                 "evidence": f"qa/test_cli.py ({name})", "error": str(e)}
            print(f"FAIL {story_id} {name}: {e}")

    RESULTS.write_text(json.dumps({"results": results}, indent=2), encoding="utf-8")
    passed = sum(1 for r in results.values() if r["ok"])
    print(f"\n{passed} passed, {len(results) - passed} failed, {len(results)} run")
    return 1 if passed != len(results) else 0


def _last_id(out):
    for tok in reversed(out.replace("\n", " ").split()):
        t = tok.strip("'\"{},")
        if len(t) == 32 and all(c in "0123456789abcdef" for c in t.lower()):
            return t
    return ""


if __name__ == "__main__":
    sys.exit(main())
