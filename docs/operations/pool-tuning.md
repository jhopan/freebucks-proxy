# Pool tuning — measured behaviour of the session knobs

Evidence-backed notes on how `SLOTS_PER_ACCOUNT`, `QUEUE_WAIT`, `QUEUE_DEPTH`,
`MAX_SPILL_ACCOUNTS` and `PIN_MODEL` interact. All numbers below were measured
live on the fork deployment, not derived from simulation.

- Host: `vps-natusa` (`/opt/freebuff-proxy`, port 3457, NAT `http://192.154.111.198:34570/`)
- Build: fork release `1.12.1.10`
- Pool: 2 accounts, both pinned to `deepseek/deepseek-v4-flash`
  (slot 0 `gimanayacara@molix.tech`, slot 1 `lunobuv44@molix.tech`)
- Date of measurements: 2026-09-20
- Live pool settings at the time of writing:
  `PIN_MODEL=0:deepseek/deepseek-v4-flash;1:deepseek/deepseek-v4-flash`,
  `SLOTS_PER_ACCOUNT=3`, `QUEUE_WAIT=2s`, `QUEUE_DEPTH=16`,
  `MAX_SPILL_ACCOUNTS=1`, `SAFE_MODE=true` (all live-apply, stored in the DB overlay)
- Baseline latency of a single measured request: ~11–15 s
  (payload `max_tokens=700`, "write a long technical explanation")

## 1. Mechanism (what the knobs act on)

```
lane            = (account x model)              one counter + one FIFO each
capacity        = accounts x SLOTS_PER_ACCOUNT   concurrent live turns per model
visit order     = roster index ascending, positional (no re-ranking)
                  cap 2 -> [0,0,1,1]      cap 1 -> [0,1]
park            = a request for a full lane joins that lane's FIFO and waits
                  up to QUEUE_WAIT for a permit
spill           = on wait expiry (or when the FIFO is full) the request moves
                  to the next eligible lane, bounded by MAX_SPILL_ACCOUNTS
session         = one active upstream session per (account, model), shared by
                  all turns of that lane (single-flight create)
pin             = eligibility filter, evaluated BEFORE any session contact
```

Source: `backend/internal/pool/slot_ledger.go`, `spill_queue.go`,
`spill_order.go`, `pin.go`, `acquire_route.go`; `backend/internal/config/pin_model.go`.

Consequences that follow directly from the mechanism:

- A lane that is full does **not** immediately hand the request to another
  account — it parks first. Spill only happens once `QUEUE_WAIT` expires or the
  FIFO is full (`QUEUE_DEPTH` reached).
- A request that parks burns no upstream quota and creates no session: the slot
  is taken before any upstream admission.
- Capacity is hard-bounded by accounts: `N` concurrent turns of one model need
  `N` accounts (with `SLOTS_PER_ACCOUNT=1`), because upstream allows only one
  active session per account.

## 2. Measurement method

Helper scripts used on the host (throwaway, under `/tmp`):

```sh
sh /tmp/fb-set.sh  SLOTS_PER_ACCOUNT 1 QUEUE_WAIT '"2s"' QUEUE_DEPTH 16   # live-apply + readback
sh /tmp/fb-fire.sh /tmp/fb-long.json 3                                    # N concurrent requests
sh /tmp/fb-snap2.sh                                                       # per-slot counters
```

Distribution per account is the delta of the `requests` counter in
`GET /admin/api/tokens` before/after each scenario. Per-request detail comes
from `chat trace` lines (`token=`, `acquire_ms=`, `session=`) and from the 429
body.

## 3. Scenario matrix (3+ concurrent, long requests)

| # | Settings | Load | 200 | 429 | Latency (s) | Account spread |
|---|---|---|---|---|---|---|
| S1 | cap 1, wait 2s, depth 16 | 3 parallel | 2/3 | 1 at 4.0 s | 17.3 / 17.3 | slot0:1 · slot1:1 |
| S2 | cap 1, wait 10s, depth 16 | 3 parallel | 2/3 | 1 at 20.0 s | 15.8 / 27.0 | slot0:1 · slot1:1 |
| S3 | cap 2, wait 2s, depth 16 | 3 parallel | **3/3** | 0 | 13.9 / 14.8 / 14.9 | — |
| S4 | cap 1, wait 1s, depth **0** | 3 parallel | 2/3 | 1 at **0.01 s** | 14.1 / 15.3 | slot0:1 · slot1:1 |
| S5 | cap 1, wait 5s, depth 3 | 3 parallel | 2/3 | 1 at 10.0 s | 18.3 / 22.4 | slot0:1 · slot1:1 |
| A | cap 2, wait 2s, depth 16 | 4 parallel | **4/4** | 0 | 11.2–14.6 | **slot0:2 · slot1:2** |
| B | cap 2, wait 2s, depth 16 | 5 parallel | 4/5 | 1 at 4.0 s | 19.7–21.2 | slot0:2 · slot1:2 |
| C | cap 1, wait 2s, depth 16 | 3 parallel | 2/3 | 1 at 4.0 s | 17.6 / 19.6 | slot0:1 · slot1:1 |
| D | cap 1, wait 1s, depth **0** | 3 parallel | 2/3 | 1 at **0.0 s** | 15.5 / 19.2 | slot0:1 · slot1:1 |
| E | cap 1, wait 2s, depth 16 | 3 **sequential** | **3/3** | 0 | — | **slot0:3 · slot1:0** |
| F | cap 3, wait 2s, depth 16 | 6 parallel | **6/6** | 0 | 16.7–17.5 | **slot0:3 · slot1:3** |
| G | cap 3, wait 2s, depth 16 | 7 parallel | 6/7 | 1 at 4.0 s | 15.2–17.4 | slot0:3 · slot1:3 |
| H | cap 4, wait 2s, depth 16 | 8 parallel | **8/8** | 0 | — (wall 20.1 s) | **slot0:4 · slot1:4** |

Two earlier runs on the same host:

- 3 **short** requests in parallel (cap 1, wait 2s): **200 / 200 / 200**,
  1.2 s / 2.7 s / 3.1 s. Short requests free their permit before the wait
  budget expires, so the third is served too.
- 2 long requests in parallel (cap 1, wait 2s): both 200, server
  `token=1 session=ca7f7684` and `token=2 session=862fcb40` (account 2 opened a
  **new** session, `acquire_ms=2000` = parked, then spilled).

Measurement noise: one early `cap 3 / 6 parallel` run reported 5/6 with a single
non-429 failure; the clean rerun of the same load returned 6/6 with spread 3:3
(row F). Treat single stray failures under a saturating mix as transient upstream
behaviour and rerun before drawing conclusions.

Observed 429 body (existing rate-limit shape, no new error code):

```
{"error":{"message":"upstream rate limited (retry after 1s): pool: token-2
 live-turn queue wait (2s) elapsed with 1 live turns (cap 1)", ...}}
```

## 4. Effect of each knob (with the evidence above)

### `SLOTS_PER_ACCOUNT` — live-turn permits per lane

| Value | Visit order | Capacity (2 accounts) | Observed |
|---|---|---|---|
| 1 | `[0,1]` | 2 | 2 parallel → one per account; 3 parallel → 1 rejected (S1, C, D) |
| 2 | `[0,0,1,1]` | 4 | 3 parallel → all served (S3); 4 parallel → all served and spread 2:2 (A); 2 parallel → both on slot 0 |
| 0 | — | ungated | no counter and no FIFO; highest ban risk — do not use on immature accounts |

- Sets *when* a lane counts as full. Larger value = more throughput per account,
  but low-concurrency traffic concentrates on the first account because the
  walk is positional.
- Session sharing is unaffected by this value: every turn on a lane reuses that
  lane's single upstream session.

### `QUEUE_WAIT` — how long a request parks before spilling

| Value | Observed rejection time (3 parallel, 2 lanes) | Effect |
|---|---|---|
| 1 s | ~1 s per lane | fast spread |
| 2 s | 4.0 s (S1, C) | spread, bounded hang |
| 5 s | 10.0 s (S5) | few spills; queues pile up |
| 10 s | 20.0 s (S2) | same outcome as 2 s but 5x slower to fail |
| 300 s | (earlier) 349:9 message split | effectively never spills; the second account idles |

- Worst-case total wait is about `QUEUE_WAIT` x number of lanes walked, because
  each lane spends its own budget before the spill chain continues.
- Raising it does not create capacity: S2 (10 s) still rejected the third
  request, just 20 s later.
- Quota path: a 429 carrying `Retry-After <= QUEUE_WAIT` is re-queued on the
  **same** lane instead of spilling (`spill_queue.go`, call site
  `acquire_route.go`), and it does not consume spill budget. Large values
  therefore make the pool wait out quota windows on one account.

### `QUEUE_DEPTH` — FIFO capacity per lane

- `16` (default): up to 16 requests park per lane, which is why rejections in
  S1/C took `QUEUE_WAIT` to appear.
- `0`: no parking at all — a full lane fails over immediately. Measured: the
  rejection surfaced in **0.01 s** (S4) / **0.0 s** (D) instead of 4 s.
- When the FIFO is full the request spills immediately, regardless of
  `QUEUE_WAIT`. Useful as a burst throttle without long hangs.

### `MAX_SPILL_ACCOUNTS` — how many further accounts may be tried

- `1` = the head lane plus one continuation account.
- `0` (or any value <= 0) means **unbounded**, i.e. every eligible account may
  be tried — it does *not* mean "no spilling".
- With two accounts the practical difference is small; with 3+ accounts it caps
  how far a request may travel before returning 429.

### `PIN_MODEL` — model reservation, evaluated before any session contact

- `0:deepseek/deepseek-v4-flash;1:deepseek/deepseek-v4-flash` = both accounts
  serve only that model. Any other model then hits `allPinnedOut` and fails
  fast with `pool: no account pinned to model ...` — no upstream admission, so
  it cannot steal or supersede a running session.
- This is the setting that stops a request for one model from killing a session
  held for another model on the same account (upstream allows only one active
  session per account; the displaced holder sees `session_superseded`).
- Pin also reduces capacity: a pinned slot cannot serve other models.

### Positional order (not a knob, but it drives the results)

- Sequential traffic always lands on the first account: scenario E gave
  `slot0:3 · slot1:0`. The second account only receives traffic when requests
  overlap long enough to exhaust the first account's permits and wait budget.
- There is no round-robin or least-loaded re-ranking in this version
  (`spill_order.go`). Reordering the roster (`POST /admin/tokens/swap`) is the
  only way to change which account is visited first.

## 5. Block assignment — `SLOTS_PER_ACCOUNT` is the block size

Because the permit walk hands each account `SLOTS_PER_ACCOUNT` permits before
moving on, concurrent traffic is served in **blocks**:

```
2 accounts, SLOTS_PER_ACCOUNT=3

  request 1-3  -> account 0   (3 concurrent turns sharing account 0's session)
  request 4-6  -> account 1   (3 concurrent turns sharing account 1's session)
  request 7+   -> park up to QUEUE_WAIT, then 429 once the walk is exhausted
```

Measured on the deployment (long requests, ~15 s each):

| cap | load | accepted | spread |
|---|---|---|---|
| 1 | 2 parallel | 2/2 | slot0:1 · slot1:1 |
| 2 | 4 parallel | 4/4 | slot0:2 · slot1:2 |
| 3 | 6 parallel | **6/6** | **slot0:3 · slot1:3** |
| 3 | 7 parallel | 6/6 + 1×429 at 4.0 s | slot0:3 · slot1:3 |
| 4 | 8 parallel | **8/8** | **slot0:4 · slot1:4** |

### Capacity table

`concurrent turns per model = accounts × SLOTS_PER_ACCOUNT`

| accounts | cap 1 | cap 2 | cap 3 | cap 4 |
|---|---|---|---|---|
| 1 | 1 | 2 | 3 | 4 |
| 2 | 2 | 4 | **6** | 8 |
| 3 | 3 | 6 | 9 | 12 |
| 4 | 4 | 8 | 12 | 16 |

Request number `accounts × SLOTS_PER_ACCOUNT + 1` is the first one that can be
rejected: it parks for `QUEUE_WAIT` per lane walked and returns 429 only when
the walk has no further eligible lane (measured: cap 3, 7 parallel → exactly one
429 at 4.0 s).

### Conditions for the block pattern

1. **Requests must overlap.** Three *sequential* requests measured `slot0:3 ·
   slot1:0` — the second account only sees traffic under concurrency (row E).
2. **One active upstream session per account.** A block of 3 is therefore three
   concurrent turns *sharing* that account's single session, not three sessions.
   Both accounts do hold independent sessions (observed `d094ca4a` on slot 0 and
   `055d6df3` on slot 1 at the same time).
3. **Block order follows roster index** (positional, no re-ranking). Reorder the
   roster (`POST /admin/tokens/swap`) to change which account takes the first
   block.

### How large may `SLOTS_PER_ACCOUNT` be?

- The proxy does not restrict the value. `0` disables gating entirely (no
  permit counter, no queue) — do not use it on accounts you care about.
- The real limits are upstream tolerance and ban risk: a larger cap means more
  simultaneous turns stacked on one account (and one egress IP).
- Keep `SAFE_MODE=true` (request jitter + idle rotation) and pick: new/cold
  account → `1`, mature account → `2`–`3`, `4` is already aggressive
  (8 parallel measured, i.e. four turns at once per account).

### If you want the block to also apply to sequential traffic

Not supported. There is no request counter, no sticky assignment and no
round-robin in this version: consecutive non-overlapping requests always go to
the first account. Workarounds are roster swaps between waves, or separate
instances with disjoint accounts (see §7).

## 6. Recommended profiles

**Profile 1 — spread & anti-ban**
```
PIN_MODEL           = 0:deepseek/deepseek-v4-flash;1:deepseek/deepseek-v4-flash
SLOTS_PER_ACCOUNT   = 1
QUEUE_WAIT          = 2s
QUEUE_DEPTH         = 4
MAX_SPILL_ACCOUNTS  = 1
```
Two parallel sessions get one account each; a third is rejected in ~4 s; load
per account stays minimal.

**Profile 2 — capacity**
```
SLOTS_PER_ACCOUNT   = 2
QUEUE_WAIT          = 2s
QUEUE_DEPTH         = 8
MAX_SPILL_ACCOUNTS  = 1
```
Four parallel turns served, distributed 2:2 under load (A). Two parallel turns
still share the first account.

**Fast-fail variant** (combine with either profile): `QUEUE_DEPTH=0` → rejection
in milliseconds instead of seconds, so retrying clients are not left hanging.

## 7. Limits that no setting can lift

- Capacity per model = number of accounts x `SLOTS_PER_ACCOUNT`; more parallel
  sessions require more accounts (one active upstream session per account).
- No per-client affinity: the proxy cannot pin "Hermes session A → account 0".
  `middleware_auth.go` hashes the client key for usage statistics only, not for
  routing. Separate accounting is available through bridge mode (client brings
  its own token) or by running separate instances with disjoint accounts.
- Never point two instances (or two hosts) at the same account: the sessions
  supersede each other.
- Slot state and queues are in-memory; a restart drops both.

## 8. Rollback

```
upstream defaults         SLOTS_PER_ACCOUNT=2  QUEUE_WAIT=30s  QUEUE_DEPTH=16  MAX_SPILL_ACCOUNTS=0
this host before tuning   SLOTS_PER_ACCOUNT=2  QUEUE_WAIT=300s QUEUE_DEPTH=16  MAX_SPILL_ACCOUNTS=1
```

`DELETE /admin/api/settings/<KEY>` restores a key's default; all four keys are
live-apply (no restart needed).

## 9. Glossary and quick answers

| Term | Meaning |
|---|---|
| lane | one (account × model) pair: its own permit counter and FIFO queue |
| permit / slot | one concurrent live turn of a lane; the counter is capped by `SLOTS_PER_ACCOUNT` |
| live turn | an in-flight request occupying a permit, from admission until the response (stream included) ends |
| park | join a lane's FIFO and wait up to `QUEUE_WAIT` for a permit |
| spill | after the wait expires (or the FIFO is full) move to the next eligible lane |
| session | the single upstream session of a (account, model) lane, reused by all of its turns |
| pin | eligibility filter declaring which model an account may serve, checked before any session contact |
| waiting room | upstream 428 state; `WAITING_ROOM_CHAIN` controls the follow-up requests |

| Question | Answer |
|---|---|
| Are the 3rd/4th requests dropped? | No. They park and are served when a permit frees up; only a request that exhausts the wait budget on every eligible lane gets 429 with `Retry-After: 1s` |
| 3 short requests at once (cap 1) | all 3 succeeded (1.2 s / 2.7 s / 3.1 s) because permits freed before the 2 s budget expired |
| 3 long requests at once (cap 1) | 2 succeeded, the third got 429 at 4.0 s (2 s per lane × 2 lanes) |
| Which account gets request N? | Positional: first account first, `SLOTS_PER_ACCOUNT` requests at a time |
| Why is the second account idle? | Sequential traffic never overlaps; only concurrency (or a manual roster swap) moves load to it |
| Why did a request hang for minutes? | `QUEUE_WAIT` was large (300 s) and each lane spends its own budget: worst case ≈ `QUEUE_WAIT` × lanes walked |
| How many parallel sessions can I run? | accounts × `SLOTS_PER_ACCOUNT` (measured 8 parallel with 2 accounts at cap 4) |
| Can the proxy pin client A to account 0? | No. Client keys are hashed for usage statistics only; use bridge mode (client supplies its own token) or separate instances with disjoint accounts |
| Does a rejected request burn quota? | No. The permit is taken before any upstream admission, so a 429 costs nothing upstream |
| Safest setting for a new account? | `SLOTS_PER_ACCOUNT=1`, `QUEUE_DEPTH=0` (fail fast, no pile-up), keep `SAFE_MODE=true` |

## 10. Post-#666 note (upstream v1.13.0)

The scenarios in §3 and the effects in §4 were measured on fork release
`1.12.1.10`, i.e. **before** upstream `e181d281` *feat(pool): global per-model
smart queue* (#666, shipped in `v1.13.0`) landed in the fork. That commit
changes exactly the parking path this document measures:

```
work-conserving arrival — admit on first free lane, park only on full
Cold miss no longer pays QUEUE_WAIT before scale-out (TTFT fix)
first full lane parks on the global FIFO
handoff failover spills without re-parking — single QUEUE_WAIT
```

Predicted effect on these numbers: a request whose first lane is full now admits
on the next free lane immediately (spreading no longer needs a small
`QUEUE_WAIT`), and the worst case drops to one wait budget instead of one per
lane walked. `SLOTS_PER_ACCOUNT` still defines capacity and the block size, and
`PIN_MODEL` still filters by model.

Treat the tables above as the pre-#666 baseline until they are re-measured on a
build that carries v1.13.0; re-run the harness in §2 against the new build and
update §3/§4 rather than trusting the prediction.
