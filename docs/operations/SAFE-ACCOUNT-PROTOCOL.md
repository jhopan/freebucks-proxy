# Safe-account protocol

Operational rules for running this gateway against a **real** FreeBuff account.
Written after the 2026-09-25 incident, in which a fresh account was banned
within ~20 minutes of its first proxy test.

Read this before pointing a new account at the gateway. A ban is **terminal** —
upstream's own session contract says so in as many words:

> `status: 'banned'` — "Account is banned. Returned from every endpoint so
> banned bots can't start a session at all. **Terminal** — CLI stops polling and
> shows a banned message."
> — `common/src/types/freebuff-session.ts`

There is no un-ban path and re-login does not help. The only recovery is a new
account, which is why the rules below are absolute.

---

## 1. One account, one client

The official CLI and this gateway are **two clients for one account**. They do
not merely share a machine — they share the credential file:

- Both read `~/.config/manicode/credentials.json`.
- `AUTO_DISCOVER_TOKEN` (on by default) fills an empty `AUTH_TOKENS` from that
  file, so the gateway silently adopts whatever account the CLI last logged in.

Upstream admits **one seat per account**: every admission rewrites
`active_instance_id`, and the disowned instance's next completion is refused
with `409 session_superseded`. Two live clients therefore fight over the seat —
the duplicate-client pattern the fork's own ban post-mortem (`fb986b48`) flags.

**The gateway now enforces this.** At startup it scans the process table for a
live `freebuff` / `codebuff` process and **refuses to boot** when one is found
(`backend/internal/cli/cli_clientguard.go`). The refusal names the pid and both
ways out.

Two knobs short-circuit it:

- `ADOPT_CLI_SESSION=true` — keeps the guard but makes it moot: the gateway
  adopts the CLI's session instead of creating a competing one.
- `SINGLE_CLIENT_GUARD=false` — disables the scan entirely. For hosts where the
  process table is unreadable or meaningless (CI runners, restricted
  containers). The e2e suite sets this so the tests stay host-independent.

Note the caveat on the adoption path: it trusts the pid recorded in
`freebuff-instance-owner.json`, and that file is only rewritten when the CLI's
session changes, so a CLI that has since restarted leaves a stale pid and the
check fails open. The process scan is what closes that hole.

## 2. Never call an upstream endpoint directly with a real token

**This is what killed the 2026-09-25 account.** Roughly 14 hand-run `curl`
calls to `https://www.codebuff.com/api/v1/ads` and
`/api/v1/freebuff/session` used the account's real token over a **plain curl TLS
stack while claiming `User-Agent: Freebuff-CLI/0.0.191`** — a persona
contradiction, exactly the `third_party_client` signal `TLS_FINGERPRINT` exists
to avoid. Minutes later the account returned `403 {"status":"banned"}`.

The gateway is the only thing allowed to talk to upstream, because it is the
only component that presents a self-consistent persona.

Use these instead — both are zero-cost and never claim a session:

| Need | Command |
| --- | --- |
| Is the token still valid? | `freebucks-proxy -test-token` |
| Full environment diagnosis | `freebucks-proxy -doctor` |

`-doctor` also reports the token probe, the egress region, and the TLS persona.
There is no diagnostic that requires a raw authenticated request.

## 3. TLS persona: `bun`

The gateway impersonates the CLI, which speaks Bun/BoringSSL and sends **no**
browser headers. `TLS_FINGERPRINT=bun` is the byte-accurate capture of that
ClientHello (`docs/operations/bun-1.3.14-clienthello.txt`): 17 ciphers,
`http/1.1`-only ALPN, 232-byte padding, no GREASE.

`auto` and `random` resolve to a **browser** preset carrying a browser
User-Agent and Sec-CH-UA, so the TLS/header persona says "browser" while the
request envelope says "CLI" — a contradiction the upstream admission lane
fingerprints. The same applies to the named browser presets (`chrome*`,
`safari*`, `firefox*`, `edge126`); they are deliberate WAF evasion only.

Because the doctor is opt-in, a misconfigured deployment can run this way
indefinitely — the VPS did, on `auto`. The serving path now **logs a warning on
every boot** when the persona contradicts the envelope
(`backend/internal/config/tls_persona.go`, shared with `-doctor`).

### 3a. `HTTP2_UPSTREAM` must be `false` with `bun`

The profile is only half the ClientHello; the ALPN list is the other half, and
`HTTP2_UPSTREAM` **rewrites it on every dial**. `stealth.Dialer` pins ALPN by
*replacing* the spec's own ALPN extension in place (`stealth/tls.go:102`,
`setALPN`), and the client passes `["h2","http/1.1"]` whenever the knob is on.

The Bun capture advertises `http/1.1` **alone** — visible in the raw record
(`0010000b000908 687474702f312e31`: one entry) and taken from a live CLI
0.0.194 over a MITM CONNECT. So `bun` + `HTTP2_UPSTREAM=true` yields a hello
that matches **neither** Bun nor Chrome: the extension order and ciphers are
Bun's, the ALPN list is Chrome's. JA3 is unaffected (it hashes extension types,
not ALPN values), but **JA4 reads the ALPN list**.

The knob's own rationale — issue #51, "real browsers advertise h2,http/1.1" —
is browser-specific and does not transfer to a CLI persona. With
`TLS_FINGERPRINT=bun`, set `HTTP2_UPSTREAM=false`.

Enforced like the persona rule: `-doctor` prints it and Serve logs it on every
boot (`config.ALPNPersonaWarning`). It fires only for CLI-faithful profiles, so
a browser preset (which legitimately wants h2) is not flagged.

> Do not set `TLS_FINGERPRINT` and `UPSTREAM_EGRESS_URL` together: the stealth
> dialer replaces the relay's `DialTLSContext`, so the relay is never used.
> The client warns about this.

## 4. Egress IP reputation — the boundary code cannot fix

This is the single biggest risk factor for a proxy, and **no code change
addresses it**. Upstream scores the egress IP through four providers
(ipinfo, Spur, Scamalytics, Cloudflare-Tor) for `anonymous`, `vpn`, `proxy`,
`tor`, `relay`, `res_proxy`, **`hosting`** and `service` signals. A datacenter
or VPS IP reads as `hosting`/`service`, which is precisely what this gate
targets, and can yield `403 free_mode_unavailable` or a ban.

**Run the account on a residential egress.** A VPS is the wrong host for a
free-mode account no matter how correct the protocol layer is.

Note this is distinct from the country gate: Indonesia is not in
`FREE_MODE_ALLOWED_COUNTRIES`, so an ID egress yields
`accessTier: "limited"` + `countryBlockReason: "country_not_allowed"`. That is
**by design, not a ban** — a limited tier, not a refusal.

## 5. Ad engagement must precede admission

The pre-session ad auction is not decoration. The fork's ban post-mortem found
that **reaching admission without ever fetching one** is the shape upstream
flags (the `yopanyolan6` ban, fixed by `WAITING_ROOM_CHAIN`).

`WAITING_ROOM_CHAIN` is **on by default**: after an upstream
`428 waiting_room_required` it fires one `POST /api/v1/ads` plus
`GET /api/v1/freebuff/streak` before the next session create.

The endpoint matters as much as the call. The fork previously posted to
`freebuff.com/api/ads`, which rejects the `waiting_room` surface outright:

```
400 {"error":"Invalid request body","details":{"surface":{"_errors":
  ["Invalid option: expected one of \"ios\"|\"freebuff_web_chat\"|
    \"chat_assistant\"|\"chat_assistant_sr\"|\"cli_chat\""]}}}
```

So **every** chain 400'd and the engagement never reached upstream. The correct
target is `https://www.codebuff.com/api/v1/ads`
(`backend/internal/upstream/ads.go`, fixed 2026-09-25). If you ever change the
ads origin, re-read `use-gravity-ad.ts:528-530` — the host and path are chosen
together, and the two branches have different surface enums.

## 6. Waiting room is not a ban

Do not confuse the two — they need opposite responses:

| Symptom | Meaning | Action |
| --- | --- | --- |
| `503` + growing `Retry-After` | Capacity queue. Account **healthy**. | Wait. The gateway backs off on the vendor's poll shape (20s doubling to 5m). |
| `429 ip_capped` | Too many distinct users on this egress IP. | Wait for another to end; not a quota reset. |
| `403 {"status":"banned"}` | **Terminal.** | New account. |
| `403 free_mode_unavailable` | Country / IP-privacy gate. | Change egress. |

## 7. Budget mechanics

- **Charge-once at session start.** Each model switch is a `DELETE` + a new
  admission, i.e. a **new charge**. Cycling models while testing burns the
  daily budget fast.
- **Two independent clocks, do not conflate them:**
  - `freebucks.daily` — resets **00:00 UTC** (`resetTimeZone: "UTC"`, limit 25).
  - Session-count `rateLimit` — resets at **midnight Pacific**
    (`resetTimeZone: "America/Los_Angeles"`).
- **Read prices live**; never trust a note. `deepseek/deepseek-v4-flash` was
  cost-0 on 2026-09-08 and was **15** (10 off-peak, 22:00–06:00 UTC) on
  2026-09-25.

## 8. Config hygiene: remove a banned token from the active path

A banned token left in the configuration is a **latent** 403. On the VPS the
`.env` still carried the banned account's token, and the only thing keeping the
service in bridge mode was a DB overlay row (`config:AUTH_TOKENS`, empty).
Overlay rows are easy to lose — a dashboard save, a cleared overlay, or a
restored DB snapshot would have put the banned token straight back into use.

`-doctor` now resolves the **same effective configuration as the server**,
including the DB overlay (`internal/bootcfg`, shared by Serve and the doctor),
so it can no longer report — and probe — a token the running server ignores.

When an account is banned:

1. Comment the token out of `.env` and write `AUTH_TOKENS=` explicitly — the
   exact form the dashboard writes when switching to bridge mode.
2. Keep the value in a dated backup, not in the live file.
3. Re-run `-doctor`: a banned token surfaces as
   `[FAIL] Token #N validity probe failed: upstream account banned`.

---

## Pre-flight checklist for a new account

1. **Close the official CLI** — confirm no `freebuff` / `codebuff` process is
   running. The gateway now refuses to start otherwise.
2. **Set `TLS_FINGERPRINT=bun` and `HTTP2_UPSTREAM=false`.** Watch the boot log
   for the persona and ALPN warnings; neither must appear. `HTTP2_UPSTREAM=true`
   rewrites the CLI's `http/1.1`-only ALPN into `h2,http/1.1` (§3a).
3. **Run on a residential egress**, not a VPS.
4. **Leave `WAITING_ROOM_CHAIN` at its default (on).**
5. **Verify with `-doctor`**, not with curl. Expect `0 failed`, the persona and
   ALPN rows both `[ok]`, and the egress region to read as a non-datacenter
   country. A banned token left in the config surfaces here as a `[FAIL]`.
6. **Do not switch models to "test"** — every switch is a fresh charge against a
   25-unit daily budget.
7. **Expect the waiting room on first contact.** A 503 with a `Retry-After` is a
   healthy account in a queue, not a failure. Let the gateway back off.
8. **If you see `403 {"status":"banned"}`** stop immediately — the account is
   gone and further requests cannot help.
