#!/usr/bin/env python3
"""freebuff-proxy — thin OpenAI-compatible gateway in pure Python stdlib.

Reuses the wire logic proven by tests/free-tier (client.py + suite 03/06):
admission, agent-runs START/chat/FINISH, codebuff_metadata envelope, SSE
streaming. No external dependencies — http.server + urllib only.

Endpoints:
  GET  /healthz                     -> 200 {status:ok}
  GET  /v1/models                   -> OpenAI-compatible model list
  POST /v1/chat/completions         -> streaming (default) SSE passthrough
                                       or non-stream JSON (stream=false)

Wire notes (proven upstream 2026-10-01):
  - Admission headers must carry x-freebuff-instance-id (cli:<uuid>),
    x-freebuff-multi-session, x-freebuff-purchase-continuity,
    x-freebuff-desktop-attempt-id, x-freebuff-model, wallet-spend-limit.
  - Agent IDs are base3-free-* (migrated from base2).
  - Chat UA: ai-sdk/openai-compatible/<ver>/codebuff ... runtime/browser.
  - codebuff_metadata carries run_id, trace_session_id, client_id,
    freebuff_instance_id, cost_mode=free, llm_step_number.
"""
import argparse
import json
import os
import re
import ssl
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# ---------------------------------------------------------------------------
# Wire constants (mirror tests/free-tier/client.py)
# ---------------------------------------------------------------------------
CODEBUFF_WWW = "https://www.codebuff.com"
UA_BUN = "Bun/1.3.14"
UA_CHAT = ("ai-sdk/openai-compatible/1.0.0/codebuff "
           "ai-sdk/provider-utils/3.0.39 runtime/browser")
DEFAULT_MODEL = "z-ai/glm-5.3-flash"
DEFAULT_AGENT = "base3-free-glm-5-3-flash"
AGENT_MAP = {
    "z-ai/glm-5.3-flash": "base3-free-glm-5-3-flash",
    "deepseek/deepseek-v4-flash": "base3-free-deepseek-flash",
    "upstage/solar-mini4": "base3-free-solar-mini4",
    "upstage/solar-pro4": "base3-free-solar-pro4",
    "mimo/mimo-v2.5": "base3-free-mimo-v2-5",
    "crof/kimi-k3-eco": "base3-free-kimi-k3-eco",
    "stealth/space-bunny-alpha": "base3-free-space-bunny-alpha",
}
PROXY_CANONICAL_TOOLS = 16

# Token sources (same order as client.py)
TOKEN_PATHS = [
    os.path.expanduser("D:/tmp/fb-device-token.json"),
    os.path.expanduser("~/.config/manicode/credentials.json"),
    os.path.expanduser("~/.config/freebuff/credentials.json"),
]

_g_mu = threading.Lock()
_g_token = None
_g_token_id = ""
_g_token_email = ""
_g_trace = {}  # run_id -> client_id


def discover_credentials():
    for path in TOKEN_PATHS:
        if not os.path.exists(path):
            continue
        try:
            with open(path, "r", encoding="utf-8") as f:
                data = json.load(f)
            if "authToken" in data:
                return data
            if "default" in data and isinstance(data["default"], dict) \
                    and data["default"].get("authToken"):
                return data["default"]
        except Exception:
            continue
    return None


def load_creds():
    global _g_token, _g_token_id, _g_token_email
    if _g_token:
        return _g_token, _g_token_id, _g_token_email
    creds = discover_credentials()
    if not creds:
        return None, "", ""
    _g_token = creds["authToken"]
    _g_token_id = creds.get("id", "")
    _g_token_email = creds.get("email", "")
    return _g_token, _g_token_id, _g_token_email


def request_http(method, url, headers=None, body=None, timeout=60, stream=False):
    """urllib request with redirect handling, returns (status, data-or-stream)."""
    cur_url = url
    cur_headers = dict(headers or {})
    payload = None
    if body is not None:
        payload = json.dumps(body).encode("utf-8")
        if "Content-Type" not in cur_headers:
            cur_headers["Content-Type"] = "application/json"

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None

    opener = urllib.request.build_opener(NoRedirect)
    for _ in range(4):
        req = urllib.request.Request(
            cur_url,
            data=payload if method in ("POST", "PUT", "PATCH") else None,
            headers=cur_headers,
            method=method,
        )
        try:
            resp = opener.open(req, timeout=timeout)
            if stream:
                return resp.status, resp
            raw = resp.read().decode("utf-8", "replace")
            try:
                return resp.status, json.loads(raw)
            except Exception:
                return resp.status, raw
        except urllib.error.HTTPError as e:
            if e.code in (301, 302, 307, 308) and "Location" in e.headers:
                cur_url = urllib.parse.urljoin(cur_url, e.headers["Location"])
                continue
            raw = e.read().decode("utf-8", "replace")
            try:
                return e.code, json.loads(raw)
            except Exception:
                return e.code, raw
    return 500, {"error": "too_many_redirects"}


def upstream_kwargs():
    """Additional upstream client kwargs (cert CA path override, etc.)."""
    return {}


# ---------------------------------------------------------------------------
# Session + chat wire (from suite 03, adapted to a shared runtime)
# ---------------------------------------------------------------------------
def ensure_session_admitted(token, model):
    st, sess = request_http(
        "GET",
        f"{CODEBUFF_WWW}/api/v1/freebuff/session",
        headers={"Authorization": f"Bearer {token}", "User-Agent": UA_BUN},
        timeout=30,
    )
    if st == 200 and isinstance(sess, dict):
        if sess.get("status") == "active" and sess.get("instanceId"):
            return sess["instanceId"], False
    claim_id = f"cli:{uuid.uuid4()}"
    headers = {
        "Authorization": f"Bearer {token}",
        "User-Agent": UA_BUN,
        "x-freebuff-model": model,
        "x-freebuff-wallet-spend-limit": "0",
        "x-freebuff-instance-id": claim_id,
        "x-freebuff-multi-session": "1",
        "x-freebuff-purchase-continuity": "1",
        "x-freebuff-desktop-attempt-id": claim_id[4:],
        "x-fb-timezone": "Asia/Jakarta",
        "x-freebuff-first-tab-discount": "0",
    }
    st, admit = request_http(
        "POST",
        f"{CODEBUFF_WWW}/api/v1/freebuff/session/admission",
        headers=headers,
        body=None,
        timeout=60,
    )
    if st == 200 and isinstance(admit, dict):
        return admit.get("instanceId") or claim_id, True
    if st == 409 and isinstance(admit, dict):
        holder = admit.get("currentInstanceId")
        if holder:
            return holder, False
    raise RuntimeError(f"Failed to admit session (HTTP {st}): {admit}")


def get_canonical_system_prompt():
    import datetime
    today = datetime.datetime.now().strftime("%B %d, %Y")
    return (
        "You are Buffy, the coding agent behind Codebuff. You help users with software "
        "engineering tasks: fixing bugs, adding functionality, refactoring, and explaining code.\n\n"
        f"Current date: {today}.\n\n"
        "- Match the project's existing conventions.\n"
        "- Prefer editing existing files over creating new ones.\n"
        "- Verify non-trivial changes by running the project's typecheck and relevant tests.\n"
        "- Use write_todos to plan and track multi-step tasks.\n"
        "- Your responses are displayed in a terminal. Keep them short and concise.\n"
        "- Don't run destructive commands unless the user asks for them.\n"
    )


def load_canonical_tools():
    cands = [
        os.path.join(os.path.dirname(os.path.abspath(__file__)), "cli_tools.json"),
        os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
                     "tests", "free-tier", "cli_tools.json"),
        "D:/tmp/cli_tools.json",
    ]
    for p in cands:
        if os.path.exists(p):
            try:
                with open(p, "r", encoding="utf-8") as f:
                    tools = json.load(f)
                if tools:
                    return tools
            except Exception:
                continue
    return [{}] * PROXY_CANONICAL_TOOLS


def start_agent_run(token, user_id, agent_id):
    headers = {
        "Authorization": f"Bearer {token}",
        "x-freebuff-acting-user-id": user_id,
        "Content-Type": "application/json",
        "User-Agent": UA_BUN,
    }
    body = {"action": "START", "agentId": agent_id, "ancestorRunIds": []}
    st, resp = request_http(
        "POST",
        f"{CODEBUFF_WWW}/api/v1/agent-runs",
        headers=headers,
        body=body,
        timeout=60,
    )
    if st != 200 or not isinstance(resp, dict) or "runId" not in resp:
        raise RuntimeError(f"agent-runs START failed (HTTP {st}): {resp}")
    return resp["runId"]


def finish_agent_run(token, user_id, run_id):
    headers = {
        "Authorization": f"Bearer {token}",
        "x-freebuff-acting-user-id": user_id,
        "Content-Type": "application/json",
        "User-Agent": UA_BUN,
    }
    body = {
        "action": "FINISH",
        "runId": run_id,
        "status": "completed",
        "totalSteps": 1,
        "directCredits": 0,
        "totalCredits": 0,
        "steps": [],
    }
    try:
        st, _ = request_http(
            "POST",
            f"{CODEBUFF_WWW}/api/v1/agent-runs",
            headers=headers,
            body=body,
            timeout=30,
        )
        return st
    except Exception:
        return 0


def build_chat_payload(model, instance_id, run_id, messages, client_tools):
    trace_id = str(uuid.uuid4())
    client_id = uuid.uuid4().hex[:13]
    _g_mu.acquire()
    _g_trace[run_id] = client_id
    _g_mu.release()

    sys_prompt = get_canonical_system_prompt()
    msgs = [{"role": "system", "content": sys_prompt}]
    for m in messages:
        role = m.get("role")
        content = m.get("content")
        if role == "system":
            continue  # our canonical head wins
        if isinstance(content, list):
            msgs.append({"role": role, "content": content})
        else:
            msgs.append({"role": role, "content": [{"type": "text", "text": content}]})

    tools = list(client_tools) if client_tools else load_canonical_tools()

    return {
        "model": model,
        "stream": True,
        "messages": msgs,
        "tools": tools,
        "tool_choice": "auto",
        "codebuff_metadata": {
            "run_id": run_id,
            "trace_session_id": trace_id,
            "client_id": client_id,
            "freebuff_instance_id": instance_id,
            "freebuff_multi_session": "1",
            "surface": "cli",
            "cost_mode": "free",
            "llm_step_number": "1",
            "repo_snapshot": json.dumps({"gitAvailable": True, "fileCount": 50}),
        },
        "provider": {"data_collection": "deny"},
    }


def sse_generator(resp):
    """Yield upstream SSE event-stream lines incrementally.

    urllib response iteration blocks until EOF; upstream SSE keeps the
    connection alive between chunks, so we must read() incrementally and
    split on newlines, buffering partial lines across reads.
    """
    buf = b""
    try:
        while True:
            chunk = resp.read(4096)
            if not chunk:
                break
            buf += chunk
            while b"\n" in buf:
                line, buf = buf.split(b"\n", 1)
                line = line.strip()
                if line:
                    yield line + b"\n"
    except Exception:
        pass
    finally:
        try:
            resp.close()
        except Exception:
            pass


# ---------------------------------------------------------------------------
# HTTP server
# ---------------------------------------------------------------------------
class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        print("[pyproxy] %s" % (fmt % args), flush=True)

    def _send_json(self, status, obj, headers=None):
        body = json.dumps(obj).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        if headers:
            for k, v in headers.items():
                self.send_header(k, v)
        self.end_headers()
        try:
            self.wfile.write(body)
        except Exception:
            pass

    def do_GET(self):
        if self.path == "/healthz" or self.path == "/health":
            self._send_json(200, {"status": "ok", "engine": "python-stdlib"})
            return
        if self.path.startswith("/v1/models"):
            models = [
                {"id": "z-ai/glm-5.3-flash", "object": "model", "owned_by": "freebuff"},
                {"id": "deepseek/deepseek-v4-flash", "object": "model", "owned_by": "freebuff"},
                {"id": "mimo/mimo-v2.5", "object": "model", "owned_by": "freebuff"},
                {"id": "upstage/solar-mini4", "object": "model", "owned_by": "freebuff"},
                {"id": "crof/kimi-k3-eco", "object": "model", "owned_by": "freebuff"},
                {"id": "stealth/space-bunny-alpha", "object": "model", "owned_by": "freebuff"},
            ]
            self._send_json(200, {"object": "list", "data": models})
            return
        self._send_json(404, {"error": {"message": "not found", "type": "invalid_request_error"}})

    def do_POST(self):
        if not self.path.startswith("/v1/chat/completions"):
            self._send_json(404, {"error": {"message": "not found", "type": "invalid_request_error"}})
            return
        try:
            length = int(self.headers.get("Content-Length") or 0)
            raw = self.rfile.read(length) if length else b"{}"
            req = json.loads(raw or b"{}")
        except Exception as e:
            self._send_json(400, {"error": {"message": f"bad request: {e}", "type": "invalid_request_error"}})
            return

        model = req.get("model") or DEFAULT_MODEL
        stream = bool(req.get("stream", True))
        messages = req.get("messages") or []
        client_tools = req.get("tools") or []

        token, user_id, _ = load_creds()
        if not token:
            self._send_json(502, {"error": {"message": "no credentials", "type": "gateway_error"}})
            return

        try:
            instance_id, _fresh = ensure_session_admitted(token, model)
            agent_id = AGENT_MAP.get(model, DEFAULT_AGENT)
            run_id = start_agent_run(token, user_id, agent_id)
            payload = build_chat_payload(model, instance_id, run_id, messages, client_tools)

            st, up_resp = request_http(
                "POST",
                f"{CODEBUFF_WWW}/api/v1/chat/completions",
                headers={
                    "Authorization": f"Bearer {token}",
                    "x-freebuff-acting-user-id": user_id,
                    "Content-Type": "application/json",
                    "User-Agent": UA_CHAT,
                    "Accept": "text/event-stream",
                },
                body=payload,
                stream=True,
                timeout=600,
            )
            if st not in (200, 201, 202):
                # Upstream gate: pass its status through (waiting room 503,
                # recharge 402, etc.) so clients see the real error.
                err_body = up_resp.read().decode("utf-8", "replace") if hasattr(up_resp, "read") else str(up_resp)
                self._send_json(st if 200 <= st < 600 else 502, {
                    "error": {"message": f"upstream: {err_body[:500]}",
                              "type": "upstream_error", "code": st}})
                finish_agent_run(token, user_id, run_id)
                return

            if not stream:
                # Non-streaming convenience: consume SSE fully, return JSON.
                chunks = []
                for line in sse_generator(up_resp):
                    if line.startswith("data: ") and line.strip() != "data: [DONE]":
                        try:
                            c = json.loads(line[6:])
                            delta = c["choices"][0].get("delta", {})
                            if delta.get("content"):
                                chunks.append(delta["content"])
                            if delta.get("reasoning_content"):
                                chunks.append(delta["reasoning_content"])
                        except Exception:
                            pass
                finish_agent_run(token, user_id, run_id)
                text = "".join(chunks)
                self._send_json(200, {
                    "id": f"chatcmpl-{run_id}",
                    "object": "chat.completion",
                    "created": int(time.time()),
                    "model": model,
                    "choices": [{
                        "index": 0,
                        "message": {"role": "assistant", "content": text or None},
                        "finish_reason": "stop",
                    }],
                    "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
                })
                return

            # Streaming passthrough
            self.send_response(st)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "keep-alive")
            self.send_header("X-Accel-Buffering", "no")
            self.end_headers()
            try:
                for line in sse_generator(up_resp):
                    self.wfile.write(line.encode("utf-8"))
                    self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                pass
            finally:
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()
                finish_agent_run(token, user_id, run_id)
        except Exception as e:
            msg = str(e)
            self._send_json(502, {"error": {"message": msg[:500], "type": "upstream_error"}})


def main():
    ap = argparse.ArgumentParser(description="freebuff thin python proxy")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=3457)
    ap.add_argument("--models", type=int, default=0, dest="show_models",
                    help=argparse.SUPPRESS)
    args = ap.parse_args()

    creds = discover_credentials()
    if creds:
        print(f"[pyproxy] credentials: {creds.get('email','?')} "
              f"id={creds.get('id','')[:8]} token=****{creds['authToken'][-4:]}")
    else:
        print("[pyproxy] WARN: no credentials found — chat will 502.")

    srv = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"[pyproxy] listening on http://{args.host}:{args.port}")
    print("[pyproxy] endpoints: /healthz /v1/models /v1/chat/completions")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        print("\n[pyproxy] shutdown")
        srv.shutdown()


if __name__ == "__main__":
    main()