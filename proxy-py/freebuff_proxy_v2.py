#!/usr/bin/env python3
"""freebuff-proxy — pure-Python OpenAI-compatible gateway (stdlib only).

Fitur v1:
  1. SEMUA TOOLS JALAN — rename tool klien -> nama official sebelum kirim,
     lalu restore nama asli di response (SSE + JSON). Tiru toolmap Go.
  2. MULTI-ACCOUNT — banyak token, rotate + health tracking.
  3. TIRU WIRE — admission headers, agent base3, codebuff_metadata, UA persis
     CLI resmi (terbukti lolos upstream via OpenSSL).

Endpoints:
  GET  /healthz                  -> status + account pool
  GET  /v1/models                -> model list (free tier allowlist)
  POST /v1/chat/completions      -> stream (default) / non-stream
  POST /admin/accounts           -> tambah token runtime (multi-device)
  GET  /admin/accounts           -> lihat state akun
"""
import argparse
import datetime
import json
import os
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# ---------------------------------------------------------------------------
# Wire constants (tiru CLI resmi — terbukti lolos upstream)
# ---------------------------------------------------------------------------
CODEBUFF_WWW = "https://www.codebuff.com"
UA_BUN = "Bun/1.3.14"
UA_CHAT = ("ai-sdk/openai-compatible/1.0.0/codebuff "
           "ai-sdk/provider-utils/3.0.39 runtime/browser")
DEFAULT_MODEL = "z-ai/glm-5.3-flash"
DEFAULT_AGENT = "base3-free-glm-5-3-flash"

# Free tier allowlist (proven via kimi 409 message).
FREE_MODELS = {
    "z-ai/glm-5.3-flash": {"agent": "base3-free-glm-5-3-flash", "price": 5},
    "deepseek/deepseek-v4-flash": {"agent": "base3-free-deepseek-flash", "price": 15},
    "upstage/solar-mini4": {"agent": "base3-free-solar-mini4", "price": 5},
    "upstage/solar-pro4": {"agent": "base3-free-solar-pro4", "price": 10},
    "mimo/mimo-v2.5": {"agent": "base3-free-mimo", "price": 10},
    "crof/kimi-k3-eco": {"agent": "base3-free-kimi-k3-eco", "price": 5},
    "stealth/space-bunny-alpha": {"agent": "base3-free-space-bunny-alpha", "price": 0},
}

# ---------------------------------------------------------------------------
# Tool name mapping — tiru backend/internal/convert/toolmap_request.go
# ---------------------------------------------------------------------------
CLIENT_TO_OFFICIAL = {
    "read": "read_files", "view": "read_files", "edit": "str_replace",
    "write": "write_file", "bash": "run_terminal_command",
    "execute": "run_terminal_command", "ls": "list_directory",
    "grep": "code_search", "todo": "write_todos", "todowrite": "write_todos",
    "read_file": "read_files", "write_to_file": "write_file",
    "replace_in_file": "str_replace", "execute_command": "run_terminal_command",
    "list_files": "list_directory", "search_files": "code_search",
    "apply_diff": "apply_patch", "edit_file": "str_replace",
    "search_replace": "str_replace", "search_and_replace": "str_replace",
    "codebase_search": "code_search", "update_todo_list": "write_todos",
    "read_command_output": "run_terminal_command", "editor": "str_replace",
    "fetch_web": "read_url", "search": "code_search",
    "shell": "run_terminal_command", "shell_command": "run_terminal_command",
    "local_shell": "run_terminal_command", "container_exec": "run_terminal_command",
    "exec_command": "run_terminal_command", "exec": "run_terminal_command",
    "command": "run_terminal_command", "replace_lines": "str_replace",
    "run_shell_command": "run_terminal_command", "grep_search": "code_search",
    "todo_write": "write_todos", "web_fetch": "read_url", "save_memory": "write_todos",
    "developer__shell": "run_terminal_command", "developer__bash": "run_terminal_command",
    "developer__text_editor": "str_replace", "developer__read": "read_files",
    "developer__write": "write_file", "developer__edit": "str_replace",
    "computer__execute": "run_terminal_command",
    "developer.shell": "run_terminal_command", "developer.text_editor": "str_replace",
    "readfile": "read_files", "editfile": "str_replace", "createnewfile": "write_file",
    "runterminalcommand": "run_terminal_command", "grepsearch": "code_search",
    "globsearch": "glob", "fetchurlcontent": "read_url", "searchweb": "web_search",
    "viewsubdirectory": "list_directory", "singlefindandreplace": "str_replace",
    "writefile": "write_file", "settodolist": "write_todos", "fetchurl": "read_url",
    "todos": "write_todos", "rg": "code_search", "powershell": "run_terminal_command",
    "find": "find_files", "edit-diff": "apply_patch",
    "execute_bash": "run_terminal_command", "fuzzy_search": "code_search",
    "list_dir": "list_directory", "websearch": "web_search", "webfetch": "read_url",
    "read_many_files": "read_files", "replace": "str_replace",
    "google_web_search": "web_search", "activate_skill": "skill",
    "search_file_content": "code_search",
    # Hermes / agent-loop tools
    "terminal": "run_terminal_command", "execute_code": "run_terminal_command",
    "web_extract": "read_url", "patch": "str_replace", "todo_list": "write_todos",
    "skills_list": "skill", "skill_view": "skill", "skill_manage": "read_subtree",
    "invoke_skill": "skill", "run_ipython": "run_terminal_command",
    "fetch": "read_url", "multiedit": "str_replace", "sourcegraph": "code_search",
    "strreplacefile": "str_replace", "exec_shell": "run_terminal_command",
    "fetch_url": "read_url", "web.fetch": "read_url", "grep_files": "code_search",
    "file_search": "glob", "shell_exec": "run_terminal_command",
    "agentgrep": "code_search", "file_grep": "code_search",
    "todoread": "write_todos", "todo_read": "write_todos", "multi_edit": "str_replace",
    "complete_step": "write_todos", "strreplace": "str_replace",
    # identity passthrough
    "read_files": "read_files", "write_file": "write_file",
    "str_replace": "str_replace", "run_terminal_command": "run_terminal_command",
    "glob": "glob", "code_search": "code_search", "list_directory": "list_directory",
    "web_search": "web_search", "skill": "skill", "end_turn": "end_turn",
    "write_todos": "write_todos", "ask_user": "ask_user", "apply_patch": "apply_patch",
    "read_url": "read_url", "think_deeply": "think_deeply", "set_output": "set_output",
}

BLACKLIST_RENAME = {
    "browser_back": "run_file_change_hooks", "browser_click": "ask_user",
    "browser_console": "spawn_agents", "browser_get_images": "propose_str_replace",
    "browser_navigate": "add_message", "browser_press": "cloud_plan_ready",
    "browser_scroll": "set_messages", "browser_snapshot": "lookup_agent_info",
    "browser_type": "add_subgoal", "browser_vision": "create_plan",
    "clarify": "suggest_followups", "computer_use": "apply_patch",
    "cronjob": "update_subgoal", "delegate_task": "find_files",
    "image_generate": "render_ui", "memory": "think_deeply",
    "process": "spawn_agent_inline", "session_search": "read_docs",
    "text_to_speech": "browser_logs", "vision_analyze": "gravity_index",
}

OFFICIAL_TOOL_SCHEMA = {
    "read_files": {"type": "object", "properties": {
        "paths": {"type": "array", "items": {"type": "string"}}}, "required": ["paths"]},
    "write_file": {"type": "object", "properties": {
        "file_path": {"type": "string"}, "content": {"type": "string"}},
        "required": ["file_path", "content"]},
    "str_replace": {"type": "object", "properties": {
        "file_path": {"type": "string"}, "old_string": {"type": "string"},
        "new_string": {"type": "string"}}, "required": ["file_path", "old_string", "new_string"]},
    "run_terminal_command": {"type": "object", "properties": {
        "command": {"type": "string"}, "blocking": {"type": "boolean"}},
        "required": ["command"]},
    "list_directory": {"type": "object", "properties": {
        "path": {"type": "string"}}, "required": ["path"]},
    "code_search": {"type": "object", "properties": {
        "q": {"type": "string"}}, "required": ["q"]},
    "glob": {"type": "object", "properties": {
        "pattern": {"type": "string"}, "path": {"type": "string"}}, "required": ["pattern"]},
    "find_files": {"type": "object", "properties": {"pattern": {"type": "string"}}},
    "web_search": {"type": "object", "properties": {
        "query": {"type": "string"}}, "required": ["query"]},
    "read_url": {"type": "object", "properties": {
        "url": {"type": "string"}}, "required": ["url"]},
    "skill": {"type": "object", "properties": {
        "name": {"type": "string"}}, "required": ["name"]},
    "write_todos": {"type": "object", "properties": {
        "todos": {"type": "array", "items": {"type": "string"}}}},
    "ask_user": {"type": "object", "properties": {
        "question": {"type": "string"}}, "required": ["question"]},
    "apply_patch": {"type": "object", "properties": {
        "patch": {"type": "string"}}, "required": ["patch"]},
    "end_turn": {"type": "object", "properties": {}},
    "think_deeply": {"type": "object", "properties": {}},
    "set_output": {"type": "object", "properties": {
        "output": {"type": "string"}}, "required": ["output"]},
}

# ---------------------------------------------------------------------------
# Account pool (multi-device / multi-account)
# ---------------------------------------------------------------------------
TOKEN_PATHS = [
    os.path.expanduser("~/.config/manicode/credentials.json"),
    os.path.expanduser("~/.config/freebuff/credentials.json"),
    os.path.expanduser("D:/tmp/fb-device-token.json"),
]


class Account:
    def __init__(self, token, uid="", email="", label=""):
        self.token = token
        self.uid = uid
        self.email = email
        self.label = label or (email or token[:8])
        self.state = "ready"
        self.instance_id = ""
        self.session_model = ""
        self.session_expires = 0.0
        self.cooldown_until = 0.0
        self.last_error = ""
        self.requests = 0
        self.lock = threading.Lock()

    def to_dict(self):
        return {"label": self.label, "email": self.email, "uid": self.uid,
                "state": self.state, "session_model": self.session_model,
                "requests": self.requests, "last_error": self.last_error[:120]}


class Pool:
    def __init__(self):
        self.accounts = []
        self.lock = threading.Lock()
        self._rr = 0

    def add(self, token, uid="", email="", label=""):
        with self.lock:
            for a in self.accounts:
                if a.token == token:
                    return a
            a = Account(token, uid, email, label)
            self.accounts.append(a)
            return a

    def ready(self):
        now = time.time()
        out = []
        with self.lock:
            for a in self.accounts:
                if a.state == "banned":
                    continue
                if a.state == "cooldown":
                    if a.cooldown_until > now:
                        continue
                    a.state = "ready"
                out.append(a)
        return out

    def pick(self, model):
        ready = self.ready()
        if not ready:
            return None
        for a in ready:
            if a.session_model == model and a.session_expires > time.time():
                return a
        with self.lock:
            self._rr = (self._rr + 1) % len(ready)
            return ready[self._rr]

    def mark_banned(self, acc, reason):
        with self.lock:
            acc.state = "banned"
            acc.last_error = reason

    def mark_cooldown(self, acc, seconds, reason):
        with self.lock:
            acc.state = "cooldown"
            acc.cooldown_until = time.time() + seconds
            acc.last_error = reason

    def load_from_disk(self):
        for path in TOKEN_PATHS:
            if not os.path.exists(path):
                continue
            try:
                with open(path, "r", encoding="utf-8") as f:
                    data = json.load(f)
                acct = data.get("default") if isinstance(data.get("default"), dict) else data
                if acct.get("authToken"):
                    self.add(acct["authToken"], acct.get("id", ""), acct.get("email", ""))
                    print(f"[pool] loaded {acct.get('email','?')} from {path}")
            except Exception as e:
                print(f"[pool] skip {path}: {e}")
        for tok in (os.environ.get("FB_AUTH_TOKENS") or "").split(","):
            tok = tok.strip()
            if tok:
                self.add(tok, label=f"env:{tok[:8]}")


POOL = Pool()


# ---------------------------------------------------------------------------
# HTTP helper
# ---------------------------------------------------------------------------
def request_http(method, url, headers=None, body=None, timeout=60, stream=False):
    cur_url = url
    cur_headers = dict(headers or {})
    payload = None
    if body is not None:
        payload = json.dumps(body).encode("utf-8")
        cur_headers.setdefault("Content-Type", "application/json")

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None

    opener = urllib.request.build_opener(NoRedirect)
    for _ in range(4):
        req = urllib.request.Request(
            cur_url, data=payload if method in ("POST", "PUT", "PATCH") else None,
            headers=cur_headers, method=method)
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


# ---------------------------------------------------------------------------
# Session + agent-runs wire
# ---------------------------------------------------------------------------
def ensure_session(acc, model):
    st, sess = request_http(
        "GET", f"{CODEBUFF_WWW}/api/v1/freebuff/session",
        headers={"Authorization": f"Bearer {acc.token}", "User-Agent": UA_BUN}, timeout=30)
    if st == 200 and isinstance(sess, dict):
        if sess.get("status") == "active" and sess.get("instanceId"):
            acc.instance_id = sess["instanceId"]
            acc.session_model = sess.get("model", model)
            acc.session_expires = time.time() + 3600
            return acc.instance_id, False
        if sess.get("status") == "banned":
            POOL.mark_banned(acc, "session banned")
            raise RuntimeError("banned")

    claim = f"cli:{uuid.uuid4()}"
    headers = {
        "Authorization": f"Bearer {acc.token}", "User-Agent": UA_BUN,
        "x-freebuff-model": model, "x-freebuff-wallet-spend-limit": "0",
        "x-freebuff-instance-id": claim, "x-freebuff-multi-session": "1",
        "x-freebuff-purchase-continuity": "1",
        "x-freebuff-desktop-attempt-id": claim[4:],
        "x-fb-timezone": "Asia/Jakarta", "x-freebuff-first-tab-discount": "0",
    }
    st, admit = request_http(
        "POST", f"{CODEBUFF_WWW}/api/v1/freebuff/session/admission",
        headers=headers, body=None, timeout=60)
    if st == 200 and isinstance(admit, dict):
        acc.instance_id = admit.get("instanceId") or claim
        acc.session_model = model
        acc.session_expires = time.time() + 3600
        return acc.instance_id, True
    if st == 409 and isinstance(admit, dict):
        holder = admit.get("currentInstanceId")
        if holder:
            acc.instance_id = holder
            return holder, False
    if st == 403 and isinstance(admit, dict) and admit.get("status") == "banned":
        POOL.mark_banned(acc, "admission banned")
        raise RuntimeError("banned")
    raise RuntimeError(f"admission failed HTTP {st}: {str(admit)[:200]}")


def start_run(acc, agent_id):
    st, resp = request_http(
        "POST", f"{CODEBUFF_WWW}/api/v1/agent-runs",
        headers={"Authorization": f"Bearer {acc.token}",
                 "x-freebuff-acting-user-id": acc.uid,
                 "Content-Type": "application/json", "User-Agent": UA_BUN},
        body={"action": "START", "agentId": agent_id, "ancestorRunIds": []}, timeout=60)
    if st != 200 or not isinstance(resp, dict) or "runId" not in resp:
        raise RuntimeError(f"START failed HTTP {st}: {str(resp)[:150]}")
    return resp["runId"]


def finish_run(acc, run_id):
    try:
        request_http(
            "POST", f"{CODEBUFF_WWW}/api/v1/agent-runs",
            headers={"Authorization": f"Bearer {acc.token}",
                     "x-freebuff-acting-user-id": acc.uid,
                     "Content-Type": "application/json", "User-Agent": UA_BUN},
            body={"action": "FINISH", "runId": run_id, "status": "completed",
                  "totalSteps": 1, "directCredits": 0, "totalCredits": 0, "steps": []},
            timeout=30)
    except Exception:
        pass


# ---------------------------------------------------------------------------
# Tool mapping
# ---------------------------------------------------------------------------
def map_tools_request(client_tools):
    """Return (official_tools, restore_map official->client)."""
    restore = {}
    out = []
    seen = set()
    for t in client_tools or []:
        fn = t.get("function", {}) if isinstance(t, dict) else {}
        name = (fn.get("name") or "").strip()
        if not name:
            continue
        low = name.lower()
        if low in BLACKLIST_RENAME:
            official = BLACKLIST_RENAME[low]
        elif low in CLIENT_TO_OFFICIAL:
            official = CLIENT_TO_OFFICIAL[low]
        else:
            official = name
        if official in seen:
            continue
        seen.add(official)
        restore[official] = name
        params = fn.get("parameters") or OFFICIAL_TOOL_SCHEMA.get(official) or {
            "type": "object", "properties": {}}
        out.append({"type": "function", "function": {
            "name": official,
            "description": fn.get("description") or f"{official} tool",
            "parameters": params}})
    return out, restore


def restore_tool_names(obj, restore):
    if not restore:
        return
    def fix(fn):
        if isinstance(fn, dict) and fn.get("name") in restore:
            fn["name"] = restore[fn["name"]]
    for ch in (obj.get("choices") or []):
        if not isinstance(ch, dict):
            continue
        for key in ("delta", "message"):
            seg = ch.get(key)
            if isinstance(seg, dict):
                for tc in (seg.get("tool_calls") or []):
                    if isinstance(tc, dict):
                        fix(tc.get("function"))


# ---------------------------------------------------------------------------
# Chat payload
# ---------------------------------------------------------------------------
def canonical_system_prompt():
    today = datetime.datetime.now().strftime("%B %d, %Y")
    return (
        "You are Buffy, the coding agent behind Codebuff. You help users with software "
        "engineering tasks: fixing bugs, adding functionality, refactoring, and explaining code.\n\n"
        f"Current date: {today}.\n\n"
        "- Match the project's existing conventions. Verify a library is already used in the "
        "project before employing it.\n"
        "- Prefer editing existing files over creating new ones. Make the fewest changes.\n"
        "- Verify non-trivial changes by running the project's typecheck and relevant tests.\n"
        "- Use write_todos to plan and track multi-step tasks.\n"
        "- Your responses are displayed in a terminal. Keep them short and concise.\n"
        "- Don't run destructive or hard-to-undo commands unless the user asks for them.\n")


def build_payload(acc, model, messages, official_tools, run_id):
    msgs = [{"role": "system", "content": canonical_system_prompt()}]
    for m in messages:
        role = m.get("role")
        content = m.get("content")
        if role == "system":
            continue
        if role == "tool":
            msgs.append({"role": role, "tool_call_id": m.get("tool_call_id", ""),
                         "content": content if isinstance(content, list)
                         else [{"type": "text", "text": str(content)}]})
        elif role == "assistant" and m.get("tool_calls"):
            msgs.append({"role": role, "content": content or "", "tool_calls": m["tool_calls"]})
        elif isinstance(content, list):
            msgs.append({"role": role, "content": content})
        else:
            msgs.append({"role": role, "content": [{"type": "text", "text": content or ""}]})
    return {
        "model": model, "stream": True, "messages": msgs,
        "tools": official_tools, "tool_choice": "auto",
        "codebuff_metadata": {
            "run_id": run_id, "trace_session_id": str(uuid.uuid4()),
            "client_id": uuid.uuid4().hex[:13],
            "freebuff_instance_id": acc.instance_id,
            "freebuff_multi_session": "1", "surface": "cli", "cost_mode": "free",
            "llm_step_number": "1",
            "repo_snapshot": json.dumps({"gitAvailable": True, "fileCount": 50})},
        "provider": {"data_collection": "deny"}}


def sse_lines(resp):
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
                    yield line
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

    def _json(self, status, obj):
        body = json.dumps(obj).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        try:
            self.wfile.write(body)
        except Exception:
            pass

    def do_GET(self):
        if self.path in ("/healthz", "/health"):
            self._json(200, {"status": "ok", "engine": "python-stdlib",
                             "accounts": [a.to_dict() for a in POOL.accounts]})
            return
        if self.path.startswith("/v1/models"):
            data = [{"id": m, "object": "model", "owned_by": "freebuff"} for m in FREE_MODELS]
            self._json(200, {"object": "list", "data": data})
            return
        if self.path.startswith("/admin/accounts"):
            self._json(200, {"accounts": [a.to_dict() for a in POOL.accounts]})
            return
        self._json(404, {"error": {"message": "not found", "type": "invalid_request_error"}})

    def do_POST(self):
        if self.path.startswith("/admin/accounts"):
            try:
                length = int(self.headers.get("Content-Length") or 0)
                req = json.loads(self.rfile.read(length) or b"{}")
            except Exception as e:
                self._json(400, {"error": str(e)})
                return
            tok = (req.get("token") or "").strip()
            if not tok:
                self._json(400, {"error": "token required"})
                return
            acc = POOL.add(tok, req.get("uid", ""), req.get("email", ""), req.get("label", ""))
            self._json(200, {"ok": True, "account": acc.to_dict()})
            return
        if self.path.startswith("/v1/chat/completions"):
            self._chat()
            return
        self._json(404, {"error": {"message": "not found", "type": "invalid_request_error"}})

    def _chat(self):
        try:
            length = int(self.headers.get("Content-Length") or 0)
            req = json.loads(self.rfile.read(length) or b"{}")
        except Exception as e:
            self._json(400, {"error": {"message": f"bad request: {e}",
                                       "type": "invalid_request_error"}})
            return
        model = req.get("model") or DEFAULT_MODEL
        stream = bool(req.get("stream", True))
        messages = req.get("messages") or []
        client_tools = req.get("tools") or []

        acc = POOL.pick(model)
        if acc is None:
            self._json(503, {"error": {"message": "no ready account (all banned/cooldown)",
                                       "type": "gateway_error"}})
            return
        official_tools, restore = map_tools_request(client_tools)
        agent = FREE_MODELS.get(model, {}).get("agent", DEFAULT_AGENT)

        try:
            with acc.lock:
                acc.requests += 1
            ensure_session(acc, model)
            run_id = start_run(acc, agent)
            payload = build_payload(acc, model, messages, official_tools, run_id)
            st, up = request_http(
                "POST", f"{CODEBUFF_WWW}/api/v1/chat/completions",
                headers={"Authorization": f"Bearer {acc.token}",
                         "x-freebuff-acting-user-id": acc.uid,
                         "Content-Type": "application/json",
                         "User-Agent": UA_CHAT, "Accept": "text/event-stream"},
                body=payload, stream=True, timeout=600)
            if st not in (200, 201, 202):
                body = up.read().decode("utf-8", "replace") if hasattr(up, "read") else str(up)
                low = body.lower()
                if "banned" in low:
                    POOL.mark_banned(acc, f"chat {st} banned")
                elif st in (429, 503):
                    POOL.mark_cooldown(acc, 60, f"chat {st} busy")
                finish_run(acc, run_id)
                self._json(st if 200 <= st < 600 else 502, {
                    "error": {"message": f"upstream: {body[:400]}",
                              "type": "upstream_error", "code": st}})
                return
            if not stream:
                content, tcs = [], {}
                for line in sse_lines(up):
                    if not line.startswith(b"data:"):
                        continue
                    ps = line[5:].strip()
                    if ps == b"[DONE]":
                        break
                    try:
                        c = json.loads(ps)
                        restore_tool_names(c, restore)
                        d = c["choices"][0].get("delta", {})
                        if d.get("content"):
                            content.append(d["content"])
                        for tc in (d.get("tool_calls") or []):
                            idx = tc.get("index", 0)
                            slot = tcs.setdefault(idx, {"id": "", "type": "function",
                                                        "function": {"name": "", "arguments": ""}})
                            if tc.get("id"):
                                slot["id"] = tc["id"]
                            f = tc.get("function", {})
                            if f.get("name"):
                                slot["function"]["name"] = f["name"]
                            if f.get("arguments"):
                                slot["function"]["arguments"] += f["arguments"]
                    except Exception:
                        continue
                finish_run(acc, run_id)
                msg = {"role": "assistant", "content": "".join(content) or None}
                if tcs:
                    msg["tool_calls"] = list(tcs.values())
                self._json(200, {
                    "id": f"chatcmpl-{run_id}", "object": "chat.completion",
                    "created": int(time.time()), "model": model,
                    "choices": [{"index": 0, "message": msg,
                                 "finish_reason": "tool_calls" if tcs else "stop"}],
                    "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}})
                return
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "keep-alive")
            self.send_header("X-Accel-Buffering", "no")
            self.end_headers()
            try:
                for line in sse_lines(up):
                    if line.startswith(b"data:"):
                        ps = line[5:].strip()
                        if ps and ps != b"[DONE]":
                            try:
                                c = json.loads(ps)
                                restore_tool_names(c, restore)
                                line = b"data: " + json.dumps(c).encode("utf-8")
                            except Exception:
                                pass
                    self.wfile.write(line + b"\n\n")
                    self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                pass
            finally:
                try:
                    self.wfile.write(b"data: [DONE]\n\n")
                    self.wfile.flush()
                except Exception:
                    pass
                finish_run(acc, run_id)
        except RuntimeError as e:
            self._json(502, {"error": {"message": str(e)[:300], "type": "upstream_error"}})
        except Exception as e:
            self._json(502, {"error": {"message": str(e)[:300], "type": "gateway_error"}})


def main():
    ap = argparse.ArgumentParser(description="freebuff python proxy")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=3457)
    args = ap.parse_args()

    POOL.load_from_disk()
    print(f"[pyproxy] accounts loaded: {len(POOL.accounts)}")
    for a in POOL.accounts:
        print(f"[pyproxy]   - {a.email or a.label}")
    if not POOL.accounts:
        print("[pyproxy] WARN: no accounts (POST /admin/accounts or FB_AUTH_TOKENS env).")

    srv = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"[pyproxy] listening on http://{args.host}:{args.port}")
    print("[pyproxy] endpoints: /healthz /v1/models /v1/chat/completions /admin/accounts")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        print("\n[pyproxy] shutdown")
        srv.shutdown()


if __name__ == "__main__":
    main()
