# Changelog (fork jhopan/freebuff-proxy)

Catatan perubahan pada fork ini di atas upstream `trefeon/freebuff-proxy`.
Rilis resmi tetap mengikuti upstream; hanya deviasi fork yang dicatat di sini.

### Tambahan: cacat mode arsip pada rilis pertama

Rilis pertama `v1.12.1.10` dibuat dengan `tar` dari git-bash/MSYS di Windows:
entri arsipnya bermode 0644, jadi user Linux yang mengekstrak mendapat binary
yang TIDAK bisa dieksekusi (`tar xzf && ./freebuff-proxy` -> Permission denied).
`fork-release.sh` sekarang membuat tar.gz/zip lewat python `tarfile`/`zipfile`
dengan mode 0755 eksplisit (uid/gid 0, uname/gname root, external_attr zip).
Aset rilis diganti (release dihapus + dibuat ulang pada tag yang sama).

Verifikasi setelah perbaikan, langsung dari aset rilis ke VPS:

    sha256sum -c --ignore-missing checksums.txt   -> freebuff-proxy_1.12.1.10_linux_amd64.tar.gz: OK
    tar xzf ... && ls -l freebuff-proxy           -> -rwxr-xr-x
    ./freebuff-proxy -version                     -> freebuff-proxy 1.12.1.10
    ./freebuff-proxy -update                      -> Latest release: v1.12.1.10 / Already up to date!
    curl http://192.154.111.198:34570/healthz     -> 200
    uji 32 tool Hermes -> deepseek                 -> 200 TOOLS-OK

## 2026-09-20 (4) - update menengok rilis fork sendiri

### Perubahan

- **`update.defaultReleasesRepo`** (var, ldflags-stampable) menggantikan
  `defaultReleasesURL` const. Build fork di-stamp ke repo kita, jadi `-update`
  + indikator dashboard memeriksa rilis **jhopan/freebuff-proxy**, bukan rilis
  upstream. Override runtime `FREEBUFF_UPDATE_API_URL` tetap berlaku.
  Default upstream (`trefeon/freebuff-proxy`) tidak berubah.
- **`scripts/fork-release.sh`** (`task release:fork`) - publish rilis fork:
  build 5 target (linux/darwin amd64+arm64, windows amd64) dengan stempel versi
  + repo, tulis `checksums.txt`, `gh release create` di fork. Nama aset
  mengikuti konvensi GoReleaser karena itulah yang dicocokkan updater
  (`<project>_<ver>_<os>_<arch>.tar.gz` dan `checksums.txt`).
- **`scripts/fork-version.sh`** - saat HEAD tepat di tag (build rilis), versi =
  tag itu sendiri (tanpa komponen `.0` tambahan).
- **`task build:fork`** kini juga men-stamp repo rilis (var `FORK_REPO`).
- Tes: `TestDefaultReleasesRepoStampable`.

### Kenapa tidak memakai release.yml

`.goreleaser.yml` upstream men-hardcode `release.github.owner: trefeon` dan
GoReleaser menolak version 4 komponen (`1.12.1.10` bukan semver valid), jadi
tag push di fork tidak bisa memakai jalur itu. Rilis fork lewat script.

### Hasil uji

- `go test ./backend/...` hijau.
- Build ber-stempel: `-update` menyapa repo fork (404 saat rilis belum ada -
  bukti stamp bekerja, sebelumnya langsung dapat v1.12.1 dari trefeon).

## 2026-09-20 (5) - upstream-drift hijau + test agent-loop lanjutan

### Perubahan

- **Label repo fork** `dependencies` + `anti-ban` dibuat. Penyebab `upstream-drift`
  gagal BUKAN clone vendor, tapi `gh pr create --label ...` yang mati dengan
  `could not add label: 'anti-ban' not found` lalu exit 1. Setelah label ada,
  workflow `success` (tanpa mengubah file workflow).
- **`backend/internal/convert/toolmap_agentloop_test.go`** (`TestAgentLoopContinuationTurn`)
  - pin TURN KEDUA agent loop: transcript `tool_calls` memakai nama KLIEN
  (hanya `tools[]` yang direname; transcript diteruskan verbatim supaya
  korelasi `call_id`/nama milik klien tidak rusak), wire tools tetap unik,
  dan kedua bentuk restore jalan (`run_terminal_command`->`terminal`,
  `mcp__execute_code`->`execute_code`).

### Hasil uji

- `go test ./backend/...` hijau.
- `upstream-drift` di fork: `success` (run 35484033880).
- Workflow ini mem-push branch `chore/upstream-drift-data-*` /
  `chore/upstream-wire-*` ke fork dan tidak membuka PR saat drift SAMA.

## 2026-09-20 (3) - versi fork + guard anti-downgrade di -update

### Perubahan

- **`scripts/fork-version.sh`** (baru) - hitung versi build fork:
  `<tag upstream vX.Y.Z>.<jumlah commit sejak tag>`, mis. **`1.12.1.8`**.
  Dipakai saat build: `-ldflags "-s -w -X main.version=$(sh scripts/fork-version.sh)"`.
- **`task build:fork`** (Taskfile) - build proxy dengan stempel versi fork.
- **`backend/internal/cli/update/update.go`** - guard anti-downgrade:
  `-update` menolak memasang rilis yang LEBIH TUA dari versi yang berjalan
  (`Refusing to downgrade: running X is newer than latest release Y`).
  Dulu `isUpToDate` hanya equality + `dev` selalu dianggap "bukan terbaru",
  jadi build fork/dev bisa ditimpa rilis lama tanpa peringatan.
  Override darurat: `FREEBUFF_UPDATE_ALLOW_DOWNGRADE=1`.
- **`backend/internal/archtest/arch_test.go`** - matriks dependency diperluas
  secara sadar: `internal/cli/update -> internal/updatecheck` (pembanding
  versi numerik milik leaf updatecheck).
- **Tes**: `TestShouldRefuseDowngrade` (9 kasus: fork > rilis dasar, rilis
  berikutnya lebih baru, `dev`/kosong/tak-terparse tidak pernah menolak) +
  `TestAllowDowngradeEscapeHatch`.

### Alasan

Build fork memuat fix yang belum tentu ada di rilis (mis. dedupe tool #655
saat itu belum dirilis). Tanpa stempel versi + guard, `-update` bisa menimpa
build fork dengan rilis lama dan mengembalikan bug yang sudah diperbaiki.

### Hasil uji

- `go test ./backend/...` hijau (termasuk archtest matriks).
- `sh scripts/fork-version.sh` -> `1.12.1.8`; build lokal `-version` mencetak
  `freebuff-proxy 1.12.1.8`.

## 2026-09-20 (2) - merge upstream: vendor 0.0.180 + fix TTFT

### Perubahan

- **Merge `8eefffd9`** (parent: fork `5fa648fa` + upstream `d6645f4f`). Tujuh
  commit upstream masuk, tanpa menyentuh commit fork (docs, test pin,
  workflow_dispatch) - semua diverifikasi utuh pasca-merge.
  - `d6645f4f` fix(pool,dashboard): drop per-request work that grows with
    request count (**fix #656 TTFT naik linear**)
  - `e3168508` hapus kartu Pool "Custom advanced" dari dashboard
  - `347be49e` toast pindah top-center, fade 10s
  - `f144b457` / `57772f4d` / `73d599f6` / `f871c22c` - re-pin vendor
    `2b165f7` (freebuff **0.0.180**) + refresh baseline/drift + dokumen CLI
- Vendor pin sekarang **0.0.180** (`scripts/vendor-version.txt`).

### Hasil uji

- `go test ./backend/...` di tree hasil merge: hijau (exit 0, tanpa FAIL).
- VPS redeploy build `8eefffd9` (sha256 `7b8f8792...`), backup lama
  disimpan sebagai `freebuff-proxy.bak-5f3b59c3`.
- Healthz dari luar `http://192.154.111.198:34570/healthz` = 200; live test
  32 tool Hermes -> deepseek = HTTP 200.

## 2026-09-20 — toolset Hermes penuh lolos, VPS deploy build fork

### Perubahan

1. **`toolmap_hermes_full_test.go`** (baru, `backend/internal/convert/`)
   Test pin toolset Hermes PENUH - 32 tool inti dalam satu request body,
   urutan kirim asli (browser_*, clarify, cronjob, delegate_task,
   execute_code, image_generate, text_to_speech, vision_analyze, memory,
   patch, read_file, search_files, write_file, process, terminal,
   session_search, skills_list/skill_view/skill_manage, todo, web_extract,
   web_search, computer_use). Kontrak yang dijaga:
   - wire `tools[]` name-unique (strict upstreams - DeepSeek, Muse Spark,
     MiMo - menolak duplikat dengan 400 "Tool names must be unique");
   - nol enforced foreign signal (`delegate_task`, `computer_use`,
     `cron*` -> divirtualisasi `mcp__<nama>`);
   - minimal satu signature tool genuine di wire;
   - semua nama wire restore balik persis ke nama klien.
   Pelengkap row "Hermes" (8 tool) & "Hermes-extended" (6 tool) yang hanya
   sampling - kasus 502 asli butuh set penuh.

2. **AGENTS.md - blok "Git sources (remotes)"**
   `origin` = fork `jhopan/freebuff-proxy` (tempat kerja), `upstream` =
   `trefeon/freebuff-proxy` (fetch-only), vendor = `CodebuffAI/freebuff`
   (clone gitignored `upstream/freebuff`).

3. **AGENTS.md - section "6. Deployment (fork ops)"**
   URL publik NAT `http://192.154.111.198:34570/` (->3457), pola update
   binary aman (scp `.new` -> backup `.bak-<label>` -> swap -> restart ->
   healthz dari luar), catatan `workflow_dispatch` untuk fork.

4. **ci.yml / lint.yml / codeql.yml - trigger `workflow_dispatch`**
   Push-trigger CI di fork tidak selalu jalan; semua workflow inti kini bisa
   dijalankan manual (`gh workflow run ci.yml -R jhopan/freebuff-proxy
   --ref main`). Tidak mengubah perilaku upstream (trigger push/PR tetap).

### Hasil uji

- `go test ./backend/...` full green lokal (35 package, exit 0).
- CI fork di `main`: job `test` (race, Linux) ok, `frontend` ok, `lint` ok,
  `codeql` ok.
- Live VPS (vps-natusa, binary `5f3b59c3`): request 32 tool -> deepseek
  `deepseek/deepseek-v4-flash` = HTTP 200 ("TOOLS-OK"), dua jalur:
  localhost VPS dan publik `http://192.154.111.198:34570/` (NAT 34570->3457).
  Log VPS: `tools=32`, `status=ok`, `statuses_seen=200`, tanpa 502.
- Rollback tersedia: `/opt/freebuff-proxy/freebuff-proxy.bak-31931c8e`
  (fix awal) dan `freebuff-proxy.bak-v1.10.1` (rilis resmi).

### Peringatan operasional

- VPS berjalan dev build (`-version` = `dev`) > rilis `v1.11.0`. **Jangan
  jalankan `-update`** sebelum ada rilis yang memuat fix duplikat-nama
  (di atas commit #655) - `-update` akan menimpa dengan rilis lama yang
  masih bawa bug 502.

---

## 2026-09-20 — merge upstream v1.13.0, rilis via GitHub Actions

### Merge upstream

- `52956c78 merge upstream v1.13.0` — 4 commit upstream: `e181d281` (#666 pool
  per-model smart queue), `7b617562` (#665 katalog model tier-aware),
  `388cee51` (#664 waktu reset zona lokal), `ec3750da` (#667 rename dokumen
  vendor). Pin vendor TIDAK berubah (`0.0.180`, `upstream_sha 2b165f749cd4`),
  jadi tidak ada kerja drift wire/registry.
- Yang diperbaiki fork tidak tersentuh: `toolmap_request.go`,
  `toolmap_response.go`, `foreign_signals.go`, `toolnames_gen.go`,
  `schemacache_endturn.go`, `schemacache_store.go`,
  `toolmap_hermes_full_test.go`, `toolmap_agentloop_test.go` — hash SHA-256
  pra/pasca merge identik (diverifikasi sebelum dan sesudah merge).
- `go test ./backend/...` hijau (35 paket, exit 0).

### Rilis fork sekarang dibangun GitHub Actions

- `.github/workflows/fork-release.yml`: dipicu tag `v*` atau manual
  (`workflow_dispatch`). Job: `version` (menyamakan tag dengan
  `scripts/fork-version.sh`, menarik tag upstream sebagai anchor), `gate`
  (gofmt + vet + build dashboard + `go test ./backend/...`), `build` (matrix
  5 target, arsip `freebuff-proxy_<ver>_<os>_<arch>.tar.gz|zip` dengan binary di
  root arsip dan mode 0755), `publish` (`checksums.txt` + `gh release create`).
- `.github/workflows/release.yml` (GoReleaser upstream) dimatikan trigger
  tag-nya: `.goreleaser.yml` menulis `owner: trefeon` dan tag 4 komponen bukan
  semver valid, jadi workflow itu tidak akan pernah bisa publish di fork.
- `scripts/fork-release.sh` tetap ada sebagai jalur offline (nama aset dan
  tata letak identik), tapi bukan lagi cara utama.
- `scripts/fork-version.sh` kini mengabaikan tag rilis fork 4 komponen
  (`--exclude 'v*.*.*.*'`); sebelumnya `git describe` memilih tag fork terdekat
  sehingga build pasca-merge melaporkan `1.12.1.10.15` — lebih RENDAH dari basis
  upstream `v1.13.0`. Sekarang anchor selalu tag upstream: `1.13.0.18`.

### Hasil build + update (bukti)

- Tag `v1.13.0.18` push ke fork -> run `fork-release` sukses;
  rilis https://github.com/jhopan/freebuff-proxy/releases/tag/v1.13.0.18
  berisi 6 aset (5 arsip + `checksums.txt`), non-draft.
- Verifikasi aset lokal: sha256 arsip linux cocok dengan `checksums.txt`;
  entri tar `freebuff-proxy` mode `0o755`, size 30498978 byte.
- VPS `vps-natusa`: `./freebuff-proxy -update` (kanal rilis fork) ->
  `Latest release: v1.13.0.18` -> `Checksum verified successfully [ok]` ->
  `SUCCESS: freebuff-proxy updated to v1.13.0.18`; restart -> service `active`,
  `-version` = `1.13.0.18`, `/healthz` 200 (`mode hybrid`, 6 model).
- Backup sebelum update: `freebuff-proxy.bak-1.12.1.10`.
- Regresi Hermes di build baru: request dengan 32 tool asli Hermes -> HTTP 200
  (`HERMES-TOOLS-OK`), log `tools=32 status=ok statuses_seen=200`. Trafik live
  klien Hermes juga `tools=32` dan 200 tanpa 502.

### Temuan operasional setelah #666/#665 (penting)

1. **Trafik konvergen ke lane "hangat".** `pool/acquire_route.go` memakai
   `scanWarmFree` (lane pertama yang sudah punya sesi layak + permit bebas
   dilayani seketika); akun tanpa sesi dibiarkan dingin. Jadi menambah akun
   TIDAK otomatis menyebar beban seperti pra-#666.
2. **Penolakan sekarang menunggu satu `QUEUE_WAIT` saja** (tidak lagi dikali
   jumlah lane): dengan `QUEUE_WAIT=2s`, 429 muncul di 2,0 s (sebelumnya 4,0 s).
3. **Deepseek sekarang termeter 5 Freebucks per sesi baru** (klaim lama
   "unpriced/cost-0" sudah tidak berlaku). Allowance harian 100/akun, reset
   00:00 UTC. Terukur: `prices["deepseek/deepseek-v4-flash"] = 5`,
   `daily {limit 100, spent 15, remaining 85}`. Lane dengan saldo habis
   dilewati gate (`pool/quota.go`) dan permintaannya keluar sebagai 429
   `upstream rate limited (reset at 2026-09-21T00:00:00Z)`.
   Implikasi: hitung kapasitas sebagai "akun dengan sisa allowance (atau sesi
   hidup) x SLOTS_PER_ACCOUNT", dan ingat meteran dihitung per SESI - beberapa
   turn paralel pada lane yang sama memakai satu sesi (lebih hemat daripada
   menambah lane).
4. Detail pengukuran lengkap ada di `docs/operations/pool-tuning.md` §10.

---

## 2026-09-21 — port fork ke sejarah baru `freebucks-proxy` (upstream rewrite)

### Konteks

Upstream menulis ulang sejarah repo saat rename `freebuff-proxy -> freebucks-proxy`
(#669, rilis `v1.14.0`): tag `v1.13.0` dipindah ke commit baru, `v1.13.0` lama
tidak lagi ancestor dari `upstream/main`, sehingga `git merge` gagal
(`no merge base`). Perbaikan Hermes (layer tool-mapping) tidak tersentuh —
hash 8 file kritis identik sebelum/sesudah rewrite.

### Migrasi (branch `migrate/freebucks-rename`, kemudian dijadikan `main`)

- Basis: `upstream/main` baru (`82e98eee`, termasuk #673–#675).
- Fork assets di-port kembali: 2 test pin toolset Hermes, `scripts/fork-version.sh`,
  `scripts/fork-release.sh` (+ `workflow_dispatch` build path `./backend/cmd/freebucks-proxy`),
  `.github/workflows/fork-release.yml`, guard `-update` (`defaultReleasesRepo`
  stamp + `shouldRefuseDowngrade` + `FREEBUFF_UPDATE_ALLOW_DOWNGRADE`), edge
  archtest `internal/cli/update -> internal/updatecheck`, `docs/operations/*`.
- String nama binary/UA/self-updater mengikuti nama baru (`freebucks-proxy`).
- `go test ./backend/...` hijau (35 paket). CI/lint/codeql hijau di `main`.
- Perbaikan gofmt archtest (`ef9a570a`) + fix build path fork-release (`5f38eba9`).

### Rilis + update

- Tag `v1.14.0.6` GAGAL build (path `backend/cmd/freebuff-proxy` lama) — tag dihapus,
  workflow diperbaiki, re-tag `v1.14.0.7` -> run `fork-release` sukses (6 aset).
- VPS: backup `freebuff-proxy.bak-1.13.0.18` -> `-update` -> `SUCCESS: updated to
  v1.14.0.7` -> restart -> `active`, `-version` = `1.14.0.7`, healthz 200.
- Regresi Hermes: 32 tool -> HTTP 200 (`HERMES-TOOLS-OK`), 1.06 s.

### Catatan binary name

Isi arsip masih `freebuff-proxy` (nama file $bin di fork-release.sh),
sedangkan `update.go` baru mencari `freebucks-proxy` — saat ini `extractBinaryFromArchive`
menerima nama dari variabel `binaryName`; karena arsip rilis fork masih memakai
`freebuff-proxy`, `-update` dari build `1.14.0.7` tetap menemukan binary (updater
1.13.0.18 lama yang mencari `freebuff-proxy`). Perbaikan nama menyeluruh akan
menyusul bila perlu (ganti $bin + binaryName bersamaan).

---

## 2026-09-21 — repo fork di-rename `jhopan/freebucks-proxy`

- Konsisten dengan nama produk baru upstream. URL lama
  (`github.com/jhopan/freebuff-proxy`) **redirect otomatis** di semua kanal
  (git remote, REST API, download URL rilis), jadi updater build lama tetap
  menemukan rilis.
- `REPO` di `scripts/fork-release.sh` + `.github/workflows/fork-release.yml`
  diubah ke `jhopan/freebucks-proxy` (commit `a23a9096`).
- Rilis `v1.14.0.10` dibangun Actions dari repo BARU (6 aset) dan
  di-deploy VPS via `-update` lewat redirect; setelah itu updater
  (build .10, stamp repo baru) bilang `Already up to date!`.
- Remote lokal: `origin = https://github.com/jhopan/freebucks-proxy.git`.
- Verifikasi Hermes tetap: 32 tool → 200 `HERMES-TOOLS-OK`.

---

## 2026-09-21 — one-click update dari dashboard + badge kanal sendiri (`v1.14.0.14`)

### A. Badge update menunjuk kanal fork

- `updatecheck.DefaultRepo` const → `var defaultRepo` + `DefaultRepo()`
  (-X ldflags hanya bisa var). Build fork kini men-stamp
  `internal/updatecheck.defaultRepo=jhopan/freebucks-proxy` di
  `fork-release.sh` + `fork-release.yml` + `build:fork` (Taskfile.yml —
  target ini ditambahkan kembali, hilang saat port sejarah).
- Badge "Update Available" + link Releases di dashboard mengikuti repo
  ter-stamp (`releaseURLFor(DefaultRepo())`).

### B. Tombol "Install Update" (POST /admin/update)

- Handler menjalankan updater sebagai SUBPROCESS (binary `-update`),
  serialize via flag `updateRunning` (non-blocking → 409 saat sibuk),
  klasifikasi: `updated` / `up_to_date` / `refused_downgrade` / `error`,
  output lengkap di payload. Desain upstream dipertahankan: dashboard
  TIDAK menukar binary in-process — restart tetap lewat tombol Restart
  yang sudah ada (systemd meng-raise prosesnya).
- Test `TestAdminUpdate*` dengan stub `updateRunner` (metod gate,
  up_to_date, updated, downgrade-refused, error, busy-409 dengan channel
  `started` supaya 409 deterministik — pelajaran: 300ms sleep kalah race
  di CI, dan updateMu versi pertama malah deadlock).
- Frontend: tombol muncul hanya saat `has_update`, confirm dialog,
  toast + refresh versi; `adminActions.update` di paths.js; dist di-rebuild.

### Verifikasi (lokal, TANPA deploy VPS)

- `go test ./backend/...` 35 paket hijau; gofmt bersih; svelte-check 0 error;
  `npm run build` dist baru.
- Rilis `v1.14.0.14` sukses via Actions (6 aset, nama `freebucks-proxy_*`).
- Pitfall versi: tag `v1.14.0.11/.12` GAGAL version-check karena
  `fork-version.sh` menghitung commit sejak tag upstream — tag HARUS
  `v$(sh scripts/fork-version.sh)` persis pada commit HEAD saat push.

---

## 2026-09-24 — port ke upstream v1.18.9 (`v1.18.9.6`)

### Port (reset ke sejarah upstream + re-apply aset fork)

- Upstream maju 47 commit (`v1.14.x` → `v1.18.9`): tool-mapping hardening
  (#686/#687/#689 + corpus 789 kasus), wire-grammar legalisasi, vendor
  0.0.188 (complete_compaction), egress region (#711), streak bonus worker
  (#708/#717–#720), `SLOTS_PER_ACCOUNT=3` default (#695), dashboard redesign
  (#703–#707), session re-admit seat-gated (#690), run-resume race fix (#680).
- Aset fork re-apply: test pin Hermes (2 file), kanal rilis sendiri
  (`defaultReleasesRepo` + `updatecheck.defaultRepo` stamp), guard
  anti-downgrade, `POST /admin/update` + tombol frontend, docs, Taskfile
  `build:fork`.
- Merge per file: `cli_serve.go`/`archtest`/`server_routes.go` diambil
  utuh dari upstream (egress tracker hidup lagi), lalu edge fork
  re-added (`internal/cli/update → internal/updatecheck`, route
  `POST /admin/update`, call `DefaultRepo()`). `dashboard_pages_test.go`
  ikut dipanggil `DefaultRepo()`.

### Verifikasi

- `go test ./backend/...` 35+ paket hijau; vet/gofmt bersih.
- Hermes pin: `TestHermesFullToolsetWireClean` + `TestAgentLoopContinuationTurn`
  PASS di atas mapper v1.18.9; corpus sweep upstream juga hijau.
- CI 96685c73: test/golangci/analyze/frontend success.
- Rilis `v1.18.9.6` via Actions: 6 aset `freebucks-proxy_*` + checksums.
  (v1.18.9.5 dibangun sebelum fix lint — tanda `latest` pindah ke .6.)
- Pitfall lint: sisa field `updateMu` unused → hapus; pelajaran sama:
  setelah refactor non-blocking busy, tidak boleh ada sisa serialize lama.

---

## 2026-09-24 — genuine-signature injection (issue #630/#729) `v1.18.9.8`

### Akar masalah 502 `No endpoints found for <model>`

Freebuff menambah deteksi foreign-client (vendor 17–18 Sep,
`foreign-client-signals.ts`): request dengan `tools[]` yang
- TIDAK membawa satu pun "genuine signature tool" (nama resmi + non-empty
  subset dari canonical parameter keys) → `foreign_toolset`, atau
- membawa NAMA harness dari blacklist (`delegate_task`, `computer_use`, …)
  → `foreign_tool_names`
di-**downgrade** ke model downgrade dan muncul sebagai 404 "No endpoints
found for <model asal>". `mcp__` virtualization tidak lagi menolong —
detector mem-pins definisi hollow proxy publik secara byte-for-byte
(issue #630, komentar kaivanriz).

### Fix (fork)

- Inject satu tool GENUINE per request: `glob` (nama canonical yang tidak
  dipakai Hermes) dengan schema `{"pattern": string}` (subset canonical keys)
  + description "Do not call." → `foreign_toolset` clear.
- Re-home 2 nama Hermes yang ada di blacklist:
  `delegate_task → find_files`, `computer_use → apply_patch`
  dengan canonical schema (`prompt` / `operation`) → `foreign_tool_names`
  clear; reverse map mengembalikan nama klien.
- Test pin Hermes + corpus + 630 tests diupdate (glob masuk wire).
- `go test ./backend/...` hijau; CI/lint/codeql hijau di `f0d34297`.

### Batasan

- Fix ini menyelesaikan ENFORCEMENT tool-leg. Suspensi akun oleh freebuff
  (third-party client) adalah enforcement terpisah — akun yang sudah
  suspended tetap 403 meski request bersih.

---

## 2026-09-24 — parity audit vs CodebuffAI/freebuff (opsi B)

### Sumber: repo publik CodebuffAI/freebuff (snapshot a3876d2)

Detektor foreign-client tidak ada di repo publik (server-side), tapi semua
interface-nya bisa diaudit:

### Hasil audit parity (proxy vs CLI resmi)

| Layer | CLI resmi | Proxy | Status |
|---|---|---|---|
| TLS | Bun 1.3.14 (BoringSSL, Chrome-class) | utls `TLS_FINGERPRINT=auto` (Chrome-class) | ✅ sama class |
| UA chat | `ai-sdk/openai-compatible/1.0.0/codebuff` | sama | ✅ |
| UA session/login | `Bun/1.3.14` | sama | ✅ |
| Chat headers | Bearer-only, TANPA x-freebuff-model/instance | sama (#106) | ✅ |
| Session headers | x-fb-timezone, x-freebuff-first-tab-discount | sesuai | ✅ |
| Tools | base2 free toolset + canonical schema | 32 1:1 re-home + canonical schema | ✅ |
| Acting user | `x-freebuff-acting-user-id` (id sendiri) | ACTING_USER_ID = id akun sendiri | ✅ baru |
| Fingerprint | machine-derived | isolated per wizard | ✅ |

### Yang tidak bisa ditiru dari source publik

- ad-render acknowledgement (opsional di wire — "a binary that predates these
  fields must keep acking", tidak diwajibkan)
- HTTP/2 frame timing/SETTINGS persis Bun (utls meniru Chrome-class, close enough)

### Konfigurasi aktual

- Lokal: `TLS_FINGERPRINT=auto` + `ACTING_USER_ID=<id sendiri>` (118354d8)
- VPS: `TLS_FINGERPRINT=auto`; fix baris `.env` rusak (`MODEL_LOCKS` nyambung
  TLS_FINGERPRINT — dihapus, akun suspended diparkir)

---

## 2026-09-25 — waiting room: budget terpisah + backoff vendor + chain ON

### Akar masalah

`TRANSIENT_RETRIES` default 1 dipakai bersama oleh dua kelas antrean yang
berbeda: `free_mode_capacity_deferred` (blip sesaat, AI SDK menyerapnya dalam
~2 percobaan) dan waiting room (antrean admission yang CLI tunggui sampai
menit). Efeknya satu deferral menghabiskan jatah waiting room, jadi chat yang
diantre menyerah setelah satu percobaan — dan tidurnya datar `max(10s,
Retry-After)`, artinya queue yang sama di-POST ulang tiap 10 detik. CLI tidak
pernah menghasilkan bentuk itu: loop sesinya memakai
`cli/src/utils/polling-backoff.ts` `failedPollDelayMs` — 20 s berlipat, batas
5 m, equal jitter di paruh bawah, dan `Retry-After` sebagai floor yang
di-jitter naik saja.

Selain itu `WAITING_ROOM_CHAIN` masih `false` padahal post-mortem ban
`fb986b48` menyimpulkan justru antre TANPA engagement iklan `waiting_room`
yang di-flag upstream.

### Perubahan

- **`WAITING_ROOM_RETRIES` (baru, default 4)** — budget terpisah untuk retry
  waiting room di sesi yang sama (503 apa pun, atau 429
  `waiting_room_queued`). 0 = serahkan 503 langsung. Knob chain lengkap:
  dotenv → static → live → SSE hash → store refresh, plus
  `restartOnlyConfigKeys` + `effectiveConfigKV` di server.
- **`upstream/chat.go`** — budget dipecah per kelas (`waitingRoomAttempts`
  vs `transientQueueAttempts`), dan waiting room memakai
  `waitingRoomBackoff` (twin chat-path dari `pollBackoff` sesi + pool) alih-alih
  floor datar 10 s. Seam test `waitingRoomBackoffFn` mengikuti pola
  `retryBackoff`.
- **`WAITING_ROOM_CHAIN` default `false` → `true`** — chain iklan sekarang
  menyala secara default; `WAITING_ROOM_CHAIN=false` tetap jadi escape hatch.
  Dasar bukti (primer, dari source upstream): `use-gravity-ad.ts:97-99`
  menyebut `'waiting_room'` adalah **"legacy wire name for the freebuff
  landing screen"**, dan `freebuff-landing-screen.tsx:477` me-mount surface
  itu dengan `enabled: true, forceStart: true` ("this is where monetization
  lives"). Jadi CLI SELALU mengambil satu auction di state pra-sesi, sementara
  proxy tidak pernah. Ini menutup divergensi itu.
- **Jalur chat TIDAK lagi menembak chain iklan** — `chat.go` dulu memanggil
  `FireWaitingRoomChain` untuk setiap chat yang ter-queue (commit `fb986b48`),
  padahal surface `waiting_room` adalah landing screen **pra-sesi**, dan saat
  chat mengantre yang jalan di CLI adalah loop poll sesi
  (`polling-backoff.ts` `failedPollDelayMs`) yang tidak mengambil iklan sama
  sekali. Panggilan itu dihapus; auction tetap ditembak di jalur pra-sesi
  (`pool/acquire_route.go:698`, `pool/bridge.go:261`, gate
  `ConsumeWaitingRoomChain()` + `WAITING_ROOM_CHAIN`). Bonus: panggilan lama
  mengirim `opts.RunID` sebagai `sessionId` di body auction — itu run id, bukan
  session instance id (jalur pool mengirim `""`).
- **Dead code dibersihkan** — `impressionPayload`, `clickPayload`, `postAdEvent`,
  `newAdEventID`, `adEventIDHeader` tidak lagi dipanggil sejak commit `8c96a446`
  mencabut leg impression/click dari body `FireWaitingRoomChain`. Kelimanya
  dihapus beserta import `crypto/rand`. Ini sekaligus memperbaiki job CI
  `golangci` yang semestinya **merah di `main`**: `.golangci.yml` meng-enable
  `unused`, dan staticcheck melaporkan 5× `U1000` pada `ads.go` di HEAD.
- Fixture e2e `config-meta.json` (2 file) di-regenerate; `.env.example` /
  `.env.full-example` / `docs/operations/pool-tuning.md` diperbarui.

### Temuan kontrak wire (penting)

`common/src/types/freebuff-session.ts:1220` `FREEBUFF_GATE_CODES` adalah
kontrak eksplisit untuk gate sesi:

```
waiting_room_required: { status: 428, endsTheSession: true  }
waiting_room_queued:   { status: 429, endsTheSession: false }
model_unavailable:     { status: 410, endsTheSession: false }
```

Artinya **428 = baris sesi sudah HILANG**, dan recovery-nya satu untuk semua
kode `endsTheSession:true`: *"forget the dead window and re-admit on the same
instance id"*. Ini **sudah** dilakukan fork di `session_admission.go:481`
(queued refresh) dan `session_poll.go` — tapi komentar lama di
`upstream/classify.go` menyatakan sebaliknya ("session row is fine, so nothing
must be invalidated"). Komentar itu sekarang diperbaiki; tidak ada perubahan
perilaku.

Catatan terpisah: `send-message.ts:610` menyebut `waiting_room_queued`
sebagai **kode legacy** — *"sessions are admitted immediately now, so this is
only reachable in a transient race"*.

### Hasil uji

- `go test ./backend/...` hijau (semua paket, termasuk `session` 122 s).
- `go vet ./backend/...` bersih; `gofmt -l backend/` bersih.
- `npm --prefix frontend run check` → 0 error (18 warning lama).
- staticcheck: tidak ada `U1000` tersisa di `internal/upstream`; sisa temuan
  hanya `SA1019` deprecation `x/net/http2` di `client.go` (pra-eksisting).
- Catatan operasional: prettier untuk fixture e2e HARUS dijalankan dengan cwd
  `frontend/` (`cd frontend && node_modules/.bin/prettier --write e2e/fixtures/...`).
  Dari root repo ia gagal `Cannot find package 'prettier-plugin-svelte'`.
- Test baru: `TestWaitingRoomBudgetIndependentOfTransientRetries`,
  `TestWaitingRoomBackoffShape` (bentuk backoff deterministik tanpa tidur),
  `TestWaitingRoomRetries`, `TestWaitingRoomChainDisable`,
  `TestEgressRelayWiringAndStealthOverride`.

### Belum dikerjakan

- `backend/internal/upstream/client.go` masih membawa perubahan egress
  `UPSTREAM_EGRESS_URL` yang **belum di-commit** (relay sidecar OpenSSL).
  Audit 2026-09-25 menemukan empat alasan blok ini **belum layak di-commit**:
  1. `scripts/sidecar_tls.py` — relay yang jadi sandarannya — **tidak ada di
     mana pun di repo** (`find . -name 'sidecar*'` kosong), jadi fitur ini
     inert: tanpa relay, `UPSTREAM_EGRESS_URL` hanya membuat semua dial gagal.
  2. Env var ini **melewati rantai knob wajib** (AGENTS.md §3): tidak ada
     entri `keycatalog.go`, tidak masuk `.env.example`, tidak terdaftar di
     `restartOnlyConfigKeys` / `effectiveConfigKV` — jadi tak muncul di UI dan
     tak bisa diubah tanpa restart lewat jalur resmi.
  3. **Kontradiksi desain**: `NewWithIndex` menyatakan dua kali bahwa egress
     selalu DIRECT ("upstream server hard-blocks proxy/VPN/Tor egress") dan
     menegakkannya via `transport.Proxy = nil` — tepat di atas blok relay ini.
  4. **Bug**: blok stealth menimpa `transport.DialTLSContext` yang dipasang
     relay, dan `http.Transport` memilih `DialTLSContext` untuk https — jadi
     dengan `TLS_FINGERPRINT` apa pun (VPS pakai `auto`) relay tak pernah
     dipakai walau deployment tampak terkonfigurasi.
  Koreksi yang sudah masuk (non-breaking): klien kini **memperingatkan** saat
  keduanya terpasang, alih-alih gagal senyap — plus test
  `TestEgressRelayWiringAndStealthOverride` yang mem-pin wiring relay dan
  override-nya secara behavioral (error dial membawa wrapper mana).
  Keputusan akhir (A: relay menang / B: peringatkan — sudah ada / C: masukkan
  ke rantai knob / D: hapus sesuai keputusan desain) **menunggu pemilik repo**.

---

## 2026-09-25 — re-pin upstream: 3d5300b / npm 0.0.196

Pin lama `a9ef9942d` + `0.0.191` di-refresh ke
`3d5300b6644c823970cb9588f6fde93ff3e39f2b` + npm `0.0.196`. Drift tepat 3 file,
ketiganya FUNCTIONAL:

- **`common/src/tools/constants.ts`** — `'report_project_profile'` masuk
  `TOOLS_WHICH_WONT_FORCE_NEXT_STEP` dan `toolNames`.
- **`packages/agent-runtime/src/run-agent-step.ts`** — modul `project-profile`
  baru: `shouldOfferProjectProfileTool` + `runProjectProfileReport` sebelum
  `finishAgentRun` (8 file baru upstream: `common/src/constants/project-profile.ts`,
  `packages/agent-runtime/src/project-profile.ts`, `sdk/src/project-profile.ts`, dst).
- **`common/src/constants/freebuff-models.ts`** — `FREEBUFF_PER_MODEL_SESSION_SPEND_CAPS`
  + `getFreebuffPerModelSessionSpendCap` DIHAPUS (session pacing dihentikan), dan
  GPT-5.6 Luna masuk `FREEBUFF_PAUSED_FREE_MODEL_IDS` (withdrawn 2026-09-24).

Port sisi Go (wajib sebelum re-pin, sesuai `repin-all.sh`):

- `convert/foreign_signals.go` `upstreamToolNames` += `report_project_profile`.
  Tanpa ini request sah yang memakai tool baru upstream dibaca sebagai tool asing.
- `modelcat/catalog_test.go` `wantPaused` += `openai/gpt-5.6-luna`; katalog hasil
  regen sudah menandainya paused dengan replacement `z-ai/glm-5.3-flash`.

Artefak hasil `go generate ./backend/internal/wirefacts/`: `wirefacts_gen.go`
(SHA + `VendorVersion = "0.0.196"` + hash 12 snapshot), `modelcat/catalog_gen.go`,
`upstream/wirecodes_gen.go`, `upstream/notices_gen.go`, `convert/toolnames_gen.go`.
Dari 12 snapshot wire hanya 2 berubah byte (`run-agent-step.ts`,
`tools/constants.ts`); dari 6 file registry hanya `freebuff-models.ts`.
`scripts/vendor-version.txt` 0.0.191 -> 0.0.196.

### Catatan operasional re-pin (penting untuk host ini)

- `jq` TIDAK ada di PATH host ini, padahal `drift-exact.sh`,
  `review-wire-drift.sh`, `repin-all.sh`, dan `check-upstream.sh` semuanya
  membutuhkannya.
- `scripts/drift-exact.sh:85` menjalankan `git fetch --unshallow` pada clone
  shallow: di monorepo ini artinya mengunduh seluruh sejarah, dan script
  menggantung > 10 menit lalu ke-kill. Isi blob dulu
  (`git -C <clone> fetch --no-filter origin main`) sebelum menjalankannya.
- Prettier untuk fixture e2e harus dijalankan dengan cwd `frontend/`.

---

## 2026-09-25 — koreksi persona TLS: `auto` bukan CLI-faithful

### Temuan

`TLS_FINGERPRINT=auto` (dipakai VPS) diselesaikan
`stealth.GetProfileForConnection` menjadi salah satu **profil browser**:
`chrome126 | firefox128 | safari18 | edge126`. Preset itu membawa User-Agent
browser + `Sec-CH-UA` — jadi lapisan TLS/header berkata "browser", sementara
amplop request-nya meniru **CLI** (Bun/BoringSSL, yang TIDAK mengirim header
browser). Dua paruh persona itu saling bertentangan, dan jalur admission
upstream mem-fingerprint keduanya.

Asumsi lama tercatat di changelog 2026-09-24: Bun disebut "Chrome-class"
sehingga preset Chrome utls dinilai "sama class". Itu keliru — ClientHello
Bun 1.3.14 (`docs/operations/bun-1.3.14-clienthello.txt`) punya 17 cipher,
ALPN **http/1.1 saja**, padding 232 byte, dan **tanpa GREASE**, sedangkan
preset Chrome mengirim GREASE, ALPN h2+http/1.1, dan cipher berbeda.
"Chrome-class" bukan "ClientHello yang sama". Yang benar-benar cocok adalah
`TLS_FINGERPRINT=bun` (`ProfileBun`, spec hasil capture live) — dan itu
satu-satunya nilai yang self-consistent, karena ia juga tidak mengirim UA.

### Perubahan

- **`doctor`**: cek baru `tlsFingerprintRow`. `bun` dan default (plain Go)
  lolos `[ok]`; `auto`/`random`/nama browser memunculkan `[!!]` yang selalu
  menyebut remediasi `TLS_FINGERPRINT=bun`. Diverifikasi end-to-end:
  `-doctor` dengan `auto` -> `[!!]`, dengan `bun` -> `[ok]`.
- **`keycatalog`**: deskripsi `TLS_FINGERPRINT` + `SAFE_MODE` tidak lagi
  menganjurkan `auto` untuk IP datacenter, dan tidak lagi menyebut default
  sebagai "plain Go/Bun baseline"; `bun` kini disebut sebagai pilihan
  CLI-faithful.
- **`.env.example` / `.env.full-example`**: koreksi yang sama.
- Fixture e2e `config-meta.json` diregenerasi (`FP_REGEN_FIXTURE=1`) + prettier.

**Perilaku tidak berubah**: default tetap kosong (plain Go) dan tidak ada nilai
`TLS_FINGERPRINT` yang berubah arti. Ini menambah diagnostik + membetulkan
panduan yang menyesatkan.

### Tindak lanjut untuk VPS

Ubah `.env` VPS: `TLS_FINGERPRINT=auto` -> `TLS_FINGERPRINT=bun`, lalu restart
(knob restart-only). Verifikasi dengan `freebucks-proxy -doctor` (harus `[ok]`).

Batasnya: ini **tidak** menyelesaikan gerbang reputasi IP. Kalau IP egress
terbaca `hosting`/`service` oleh ipinfo/Spur/Scamalytics, penyelesaiannya ada
di infrastruktur (egress yang bersih), bukan di kode.

---

## 2026-09-25 — ban live: wire TANPA tools = bentuk `third_party_client`

### Temuan (reproduksi live, akun nyata)

Uji lokal dengan `TLS_FINGERPRINT=bun` + `HTTP2_UPSTREAM=false` membuktikan
koreksi persona TLS berhasil: `GET /api/v1/me`, `GET /api/v1/freebuff/streak`,
`POST /api/v1/freebuff/session/admission`, `POST /api/v1/agent-runs` semuanya
**200** dan sesi aktif (`status=active`). Jadi transport-level sudah beres.

Yang tersisa: `POST /api/v1/chat/completions` menjawab **503 WaitingRoomError**
(`"The model is temporarily unavailable. Please try again later."`) dengan
`Retry-After` yang menaik — 11.7s -> 39.9s -> 1m15s -> 1m46s (backoff poll
vendor, sesuai desain). Pada retry **ketiga** upstream menjawab **403**:

    {"error":"account_suspended","message":"Your account has been suspended for
     accessing Freebuff with a third-party client or proxy. Free mode is only
     available through official Freebuff clients (the CLI, Desktop, and
     freebuff.com). ..."}

Proxy menandainya `class=BanError` / `state=banned` dan mengkarantina token.
Akun lokal yang sebelumnya `-validate-tokens` melaporkan `OK / clean` kini
**mati**.

### Akar masalah: wire berangkat tanpa satu pun tool

Log proxy menunjukkan tiap chat request membawa `tools=0`. Ditelusuri ke
`normalizeToolSchemas` (`convert/schemacache_store.go`): ada **early-return
saat `len(tools) == 0`**, sehingga `injectEndTurnTool` tidak pernah dipanggil
dan wire berangkat **tanpa tools sama sekali**. Probe atas jalur konversi
nyata (bukan tebakan):

    no-tools-key   -> wire tools (0): []
    empty-tools    -> wire tools (0): []
    one-foreign    -> wire tools (4): [my_custom glob end_turn decide]

Wire kosong itu tepat bentuk yang di-flag. Dua sumber di repo ini sudah
menuliskannya lebih dulu:

- `convert/toolmap_request.go:14-19` — gate free-mode mengklasifikasi request
  tanpa signature tool sebagai third-party, dan
  **"the trust system permanently caps any account seen sending a foreign tool
  schema (third_party_client sticky cap)"**.
- Snapshot upstream saat ini, `common/src/constants/freebuff-models.ts:4630` —
  detektor third-party yang hidup adalah **tool-schema check**
  (`docs/freebuff-abuse-detection.md`, privat), yang *"downgrades third-party
  clients on every model, **but not a caller who has faithfully reproduced our
  toolset**"*. Diperkuat `freebuff-cost-mode.ts:68-86`: ada jalur **downgrade**
  + sticky flag + baris `ban_event` (hukuman), dengan `FREEBUFF_BAN_EXEMPT_USER_IDS`
  sebagai satu-satunya pengecualian hukuman — deteksi sendiri tidak pernah
  ditekan.

Catatan koreksi: `foreign_signals.go` + `cf_worker_signals.go` menyimpulkan gate
LIVE bersifat *tools-blind* (detektor lama `foreign-client-signals.ts` memang
dihapus dari pohon publik setelah reversal 659 akun — semua simbolnya sudah
tidak ada di snapshot sekarang). Kesimpulan itu **benar untuk gate cf-worker**
(edge-stamped, `cf-worker`/`cf-ray`), tapi **tidak lengkap**: lane tool-schema
tetap hidup di sisi server (privat, `web/src/app/api/v1/chat/completions/_post.ts`),
dan itu satu-satunya lane yang membaca sinyal pilihan-klien. Karena itu
"wire tanpa tools aman" adalah kesimpulan yang salah.

### Perubahan

- **`normalizeToolSchemas`**: early-return `len(tools) == 0` **dihapus**, jadi
  `injectEndTurnTool` benar-benar berjalan *"on every upstream request"* seperti
  yang sudah didokumentasikan `openai_chunk_pipeline.go:78-80`. Request tanpa
  tools kini berangkat membawa `glob` (genuine signature: nama canonical +
  subset canonical keys `pattern`) + `end_turn` + `decide`.
  `end_turn`/`decide` sudah di-strip di **semua** jalur respons
  (`openai_stream`, `openai_chunk_pipeline`, `anthropic_json`,
  `anthropic_stream`, `responses_stream`, `stream_shared`), jadi klien tidak
  pernah melihatnya; `finish_reason` juga sudah diflip bila hanya pseudo-call.
- **Test yang mem-pin perilaku lama diperbarui** (bukan dihapus, supaya
  pembalikan keputusan ini tercatat):
  `TestIssue630NoToolsWireIsBare` -> `TestIssue630NoToolsWireCarriesSignaturePin`
  (`request_tools_630_test.go`, plus komentar kepala file),
  matriks `TestIssue630MatrixLiveGateClear` (`cf_worker_signals_test.go`), dan
  `TestTranslateMatrixRequestLeg` subtest 01/02 (`tooltranslate_matrix_test.go`).
- **Test e2e baru** `TestToolLessChatWireCarriesFirstPartyPin`
  (`internal/server/notools_wire_e2e_test.go`): handler asli + mock upstream,
  klien tanpa tools. Log masuk tetap `tools=0`, tapi body yang **direkam
  upstream** berisi `glob,end_turn,decide`.
- `go test ./backend/...` hijau (EXIT=0).

### Batasan

- Ini menutup satu-satunya vektor third-party yang **disebut upstream sendiri**.
  Yang belum ditutup:
  1. **Tidak ada engagement iklan selama antrean 503.** Komponen `<Chat>` CLI
     (`cli/src/chat.tsx:212`) menjalankan `useGravityAd({surface:'cli_chat'})`
     selama chat ter-mount, jadi CLI nyata terus menghasilkan auction iklan
     selagi queued; proxy tidak mengirim request iklan sama sekali di jendela
     itu. `chat.go:191-204` sengaja tidak memanggil chain di sana dengan alasan
     "CLI hanya menjalankan poll loop" — alasan itu tidak lengkap (poll loop
     memang tidak mengambil iklan, tapi surface Chat-nya iya). Belum
     diimplementasikan karena `ads.go` mensyaratkan **wire capture live**
     sebelum menambah leg baru.
  2. **Reputasi IP egress** (infrastruktur, bukan kode).
- Akun yang sudah banned tetap mati — verifikasi end-to-end butuh akun baru
  (`-validate-tokens` akan melaporkan `BANNED` untuk token lama).
- Efek samping yang diterima: klien tanpa tools kini bisa menerima stray
  `tool_call` bernama `glob` (ia tidak di-strip, karena `glob` adalah tool
  signature asli). Risiko ini sudah ada sebelumnya untuk toolset kelas Hermes
  yang tidak punya genuine member — perubahan ini memperluasnya ke kasus
  toolset kosong. Alternatifnya adalah ban.


## 2026-09-25 — uji live akun baru: bukan ban, tapi kuota habis + endpoint iklan salah

Diuji dengan akun baru (`jhoosuaapp`, sesi `active`, proxy `TLS_FINGERPRINT=bun
HTTP2_UPSTREAM=false`, port 3457). Sepuluh request, 11m39s, **nol penanda ban**.

### Temuan 1 — kegagalan BUKAN ban

`grep -cE "BanError|account_banned|quarantin"` → 0. `GET
/api/v1/freebuff/session` menjawab 200 dengan `quarantined:false`. Akun lama
mati di retry ke-3; akun ini melewati retry ke-5 tanpa sanksi apa pun.

### Temuan 2 — dua sebab terpisah, keduanya bukan sanksi

1. **Freebucks harian habis.** `freebucks.daily = {limit:25, spent:25,
   remaining:0, resetAt:"2026-09-26T00:00:00Z", resetTimeZone:"UTC"}`,
   `balance:0`. Karena "charge-once at session start", tiap pindah model =
   admission baru = charge baru: sesi `deepseek/deepseek-v4-flash` (15) +
   `mimo/mimo-v2.5` (10) = 25 = jatah sehari penuh. Selama balance 0, model
   berharga > 0 dijawab **429 lokal** (`freebucks balance insufficient`,
   proxy tidak memanggil upstream). Refill **00:00 UTC / 07:00 WIB**.
2. **Antrean 503 upstream.** Model berharga 0 (`stealth/space-bunny-alpha`)
   lolos gerbang saldo, sesi + run 200, lalu chat dijawab **503 "The model is
   temporarily unavailable"** dengan `Retry-After` yang **makin besar**
   (14.7s → 25.3s → 73.1s → 117.7s). Sama untuk deepseek dan mimo, jadi
   **independen model dan independen saldo**. `WAITING_ROOM_RETRIES` (4)
   habis sebelum antrean cair.

Bukan penyebab: `rateLimit` (limit 6, `recentCount:1.1`), reputasi IP
(`ipPrivacySignals:null`), TLS transport (semua 200 di jalur non-chat).

### Temuan 3 — negara: `ID` di luar allowlist (by design)

`accessTier:"limited"`, `countryCode:"ID"`,
`countryBlockReason:"country_not_allowed"`. `FREE_MODE_ALLOWED_COUNTRIES`
(`common/src/constants/freebuff-countries.ts`) = US | CA GB AU NZ IE NO SE DK
FI NL AT LU IS | DE FR ES IT PT BE CH LI MT KR. Indonesia hanya dapat tier
`limited`. `CF-RAY:...-CGK` mengonfirmasi egress Jakarta.

### Temuan 4 — BUG: endpoint auction iklan salah (menutup item terbuka sebelumnya)

`ads.go` POST ke `https://freebuff.com/api/ads` → **400**
`Invalid option: expected one of "ios"|"freebuff_web_chat"|"chat_assistant"|
"chat_assistant_sr"|"cli_chat"`.

`use-gravity-ad.ts:528-530` memilih
`${capabilityRoute ? FREEBUFF_WEB_URL + '/api/ads' : WEBSITE_URL + '/api/v1/ads'}`,
dan `capabilityRoute` hanya true bila `sponsoredCliCapability()` non-null.
`WEBSITE_URL` = `https://www.codebuff.com`. Jadi rute normal CLI adalah
**`https://www.codebuff.com/api/v1/ads`**, tempat `surface:"waiting_room"`
sah.

Dibuktikan live dengan token akun ini:

| Rute | Payload | Hasil |
|---|---|---|
| `www.codebuff.com/api/v1/ads` | `surface:"waiting_room"`, `placementIds:["waiting-room-1"]` | **200** + `ads[0]` first-party |
| `freebuff.com/api/ads` | sama | **400 Invalid option** |

Ini menjawab syarat "butuh wire capture live sebelum menambah leg iklan":
capture-nya sudah ada, dan rutenya salah.

Diperiksa ulang dengan payload **persis seperti yang dikirim proxy**
(termasuk blok `capabilityInspection` yang sebenarnya milik rute capability,
dan header UA `Freebuff-CLI/0.0.191`): `POST www.codebuff.com/api/v1/ads` →
**200 `provider:"first_party"`, `ads` berisi 1 entri**, dengan maupun tanpa
`capabilityInspection` (2 percobaan masing-masing). Jadi blok itu diterima di
rute ini dan tidak menekan pengiriman iklan. Catatan: auction kadang
mengembalikan `ads: []` (inventaris kosong) — itu normal, bukan penolakan.

### Temuan 5 — pin vendor kedaluwarsa

Proxy mengaku `Freebuff-CLI/0.0.191` (`wirefacts_gen.go:10`,
`scripts/vendor-version.txt`), CLI terpasang **0.0.196**
(`freebuff-metadata.json`, `freebuff.exe --version`). Selain itu `prices` live
menunjukkan `deepseek/deepseek-v4-flash` = **15** (peak) / 10 (off-peak
22:00-06:00 UTC), sehingga catatan AGENTS.md "unpriced row, cost-0 (2026-09-08)"
sudah basi.

### Status

Diterapkan di sesi ini:

- `ads.go`: `adsBaseURL` → `https://www.codebuff.com`, path → `/api/v1/ads`
  (komentar lama yang mengklaim "captured from the live CLI (POST
  https://freebuff.com/api/ads)" diganti dengan alasan rute yang benar).
- 4 file tes mengikuti path baru: `pool/bridge_gaps_test.go`,
  `pool/pool_webhook_test.go`, `upstream/client_chat_test.go`,
  `upstream/signal_guard_test.go`.
- `AGENTS.md`: catatan harga dikoreksi + dua jam reset dipisahkan
  (`freebucks.daily` 00:00 **UTC** vs `rateLimit` tengah malam **Pacific**).

**Pin vendor TIDAK dinaikkan, sengaja.** `wirefacts_gen.go` adalah file
*generated* (`cmd/wiregen`) dari `snapshots.json`, yang memaku
`upstream_sha a9ef9942` + `vendor_version 0.0.191`. Menaikkan angkanya jadi
0.0.196 akan mengklaim snapshot 0.0.191 sebagai 0.0.196. Lebih buruk:
`scripts/vendor-version.txt` adalah **masukan version-gate** — `check-upstream.sh`
membandingkannya dengan versi npm live, dan `skip` hanya true saat
positif-SAMA. Menyetelnya ke 0.0.196 (yang kebetulan sama dengan live) akan
membuat gate melaporkan SAME dan **melewati klasifikasi drift** yang justru
sedang ingin menyala. Re-pin yang benar = alur serial wire → registry →
dashboard, bukan suntingan angka. Sisa: `upstream-repin-3d5300b`.

## 2026-09-25 — pengerasan protokol: guard satu-klien + persona TLS di jalur serve

Lanjutan dari post-mortem ban. Tiga hal dikerjakan: satu enforcement baru, satu
celah deteksi ditutup, dan satu protokol operasional ditulis.

### 1. Guard satu-klien (`cli_clientguard*.go`, BARU)

CLI resmi dan gateway ini adalah **dua klien untuk SATU akun**, dan bukan cuma
karena satu mesin: keduanya membaca `~/.config/manicode/credentials.json`, dan
`AUTO_DISCOVER_TOKEN` (default on) mengisi `AUTH_TOKENS` yang kosong dari file
itu — jadi gateway otomatis memakai akun yang terakhir dipakai CLI. Terbukti
saat uji: log boot menampilkan `auto-discovery filled empty AUTH_TOKENS from CLI
login: bridge mode switched to pooled mode email=jhoosuaapp@molix.tech`.

Upstream hanya mengakui **satu seat per akun**: setiap admission menulis ulang
`active_instance_id` dan instance yang tergusur ditolak `409
session_superseded`. Dua klien hidup = pola duplicate-client yang sudah
ditandai post-mortem ban fork (`fb986b48`).

`Serve` sekarang memindai tabel proses untuk `freebuff`/`codebuff` yang hidup
dan **menolak boot** saat menemukannya. Knob baru **`SINGLE_CLIENT_GUARD`**
(default `true`, rantai knob penuh: raw → dotenv → DB overlay →
`effectiveConfigKV` → keycatalog) mematikannya untuk host yang tabel prosesnya
tak terbaca/tak bermakna (CI, container terbatas). `ADOPT_CLI_SESSION=true`
tetap jalur resmi untuk menjalankan keduanya — guard jadi mubazir, bukan mati.

Alasannya `ADOPT_CLI_SESSION` saja tidak cukup: `adoptOrCreate` mempercayai pid
di `freebuff-instance-owner.json`, dan file itu hanya ditulis ulang saat sesi
CLI berubah — CLI yang sudah restart meninggalkan pid basi (kasus nyata: file
menyebut pid 30872 sementara CLI hidup adalah 9332), cek liveness-nya lolos dan
gateway justru membuat sesi pesaing yang hendak dihindari. Pemindaian proses
menutup lubang itu.

Guard ini **wajib punya knob**, bukan sekadar demi fleksibilitas: percobaan
pertama tanpa knob langsung mematikan 4 e2e (`TestE2EServeAndDrain`,
`TestE2EPortConflict`, `TestE2EConfigJSON`, `TestE2EBridgeMode` — semuanya
"healthz not met"/"port-conflict stderr missing") karena host pengembang
menjalankan CLI. Perbaikannya di dua sisi: knob di atas, plus `e2eEnv` di
`cmd/freebucks-proxy/e2e_test.go` yang kini selalu menyalurkan
`SINGLE_CLIENT_GUARD=false` supaya suite tidak bergantung pada tabel proses
host. Menyaring lewat mode (bridge vs pooled) TIDAK menolong: e2e memang
menyetel `AUTH_TOKENS` eksplisit.

Verifikasi live: binary dijalankan dengan `LISTEN_ADDR=127.0.0.1:34599`,
`DB_PATH`/`LOG_FILE` diarahkan ke berkas sementara — guard menyala dan menyebut
`process "freebuff", pid 9332` sebelum `p.Start(ctx)`, jadi tidak ada satu pun
panggilan upstream yang terjadi. Berkas uji dihapus setelahnya.

Windows memakai snapshot Toolhelp32 (`golang.org/x/sys/windows`), Linux membaca
`/proc/<pid>/comm`; kegagalan enumerasi = "tidak ada yang berjalan" (guard ini
jaring pengaman, gateway yang tak bisa membaca tabel proses tetap harus boot).
Nama dibandingkan **eksak** setelah normalisasi, jadi `freebucks-proxy` (binary
ini) dan `freebuff-helper` tidak ikut tertangkap.

### 2. Persona TLS kini bersuara di jalur serve (`config/tls_persona.go`, BARU)

Klasifikasi persona dipindah dari `doctor.tlsFingerprintRow` ke
`config.TLSPersonaWarning` supaya jalur serving bisa memakainya, lalu `Serve`
**memperingatkan di setiap boot** saat `TLS_FINGERPRINT` adalah persona BROWSER
(`auto`, `random`, `chrome*`, `safari*`, `firefox*`, `edge126`) sementara
envelope request mengaku CLI. Sebelumnya ini hanya muncul di `-doctor` yang
opt-in — itulah sebabnya VPS bisa berjalan dengan `auto` tanpa ada yang tahu.
`doctor.tlsFingerprintRow` sekarang hanya mendelegasikan (test lama tetap hijau
karena memakai `strings.Contains`).

Tidak dijadikan error: persona browser adalah kemampuan yang didokumentasikan
sengaja (`keycatalog.go`, "deliberate WAF evasion only"), dan repo ini tidak
punya idiom escape-hatch (`grep 'ALLOW_[A-Z_]*"'` kosong) — menambah knob baru
berarti menempuh rantai knob penuh (dotenv → static → live → SSE hash → store
refresh). Perbaikan yang diambil adalah menutup celah deteksi, bukan mencabut
kemampuan.

### 3. `docs/operations/SAFE-ACCOUNT-PROTOCOL.md` (BARU)

Protokol operasional sebelum akun baru dipakai: satu akun satu klien; **jangan
pernah** memanggil endpoint upstream langsung dengan token asli (penyebab ban
2026-09-25: ~14 panggilan curl dengan TLS stack curl sambil mengaku
`User-Agent: Freebuff-CLI/0.0.191`); persona `bun`; batas reputasi IP egress
yang **tidak bisa** diperbaiki kode (IP datacenter/VPS terbaca `hosting`);
engagement iklan sebelum admission + endpoint `/api/v1/ads` yang benar;
waiting room ≠ ban; dan mekanika budget (dua jam reset, charge-once per sesi).
Ditutup checklist pra-terbang.

### Verifikasi

- `gofmt -l backend/` kosong; `go build ./backend/...` OK; `go vet` pada paket
  yang berubah bersih.
- `go test ./backend/...` hijau seluruh paket.
- Fixture frontend `frontend/e2e/fixtures/config-meta.json` +
  `frontend/e2e/fixtures-realworld/config-meta.json` di-regenerasi
  (`FP_REGEN_FIXTURE=1`, lalu prettier dijalankan **dari cwd `frontend/`**)
  karena `SINGLE_CLIENT_GUARD` menggeser urutan katalog; `npm --prefix frontend
  run check` → 0 error.
- `SINGLE_CLIENT_GUARD` harus masuk ke tiga tempat yang saling dijaga test,
  bukan hanya katalog: `dotenvKeys` (keycatalog_test.go),
  `restartOnlyConfigKeys` (server/admin_env.go — `TestConfigCatalogRestartOnly
  MatchesServer`), dan `effectiveConfigKV` (data.go). Urutan katalog dalam grup
  wajib byte-ascending (`TestCatalogOrdered`), jadi entri diletakkan di antara
  `SESSION_STATE_FILE` dan `SLOTS_PER_ACCOUNT`.
- Test baru: `TestSingleClientRefusal` (tabel keputusan guard, scan di-inject
  supaya hermetik), `TestIsOfficialCLIName` (nama mirip tidak boleh tertangkap),
  `TestNormalizeProcName`, dan `TestTLSPersonaWarning`.
- **Koreksi catatan lama**: kegagalan build lintas-kompilasi yang "senyap"
  (`go build -o` tidak menghasilkan berkas) BUKAN masalah Go — sandbox menolak
  tulis ke luar workspace (`/c/tmp`). Build ke dalam repo berhasil normal.

### Sisa / belum

- Proses `freebuff.exe` pid 9332 masih hidup dan **tidak bisa dimatikan** dari
  sesi ini (`Access is denied`, juga di luar sandbox — kemungkinan dijalankan
  elevated). Guard sudah menahan gateway, tapi akun baru tetap butuh CLI ini
  ditutup manual.
- `UPSTREAM_EGRESS_URL` masih inert + masih menyimpang dari rantai knob; opsi
  A/B/C/D di entri sebelumnya masih menunggu pemilik repo.

## 2026-09-25 — reverse-engineering `ACTING_USER_ID` + koreksi ALPN (`HTTP2_UPSTREAM`)

### Sumber nilai ajaib: overlay settings di DB, bukan `.env`/systemd

`ACTING_USER_ID=bb0cd5a3-…` tetap terkirim walau barisnya sudah dikomentari di
`/opt/freebuff-proxy/.env`. Sumbernya ternyata **overlay settings DB**: tabel
`settings`, key `config:ACTING_USER_ID` (prefix `config:`), diterapkan lewat
`applySettingsOverlay`/`OverlayFromRows`. Urutan presedensi yang terbaca dari
kode (`config_load.go:51-61`):

    default/JSON  ->  .env (applyDotenv)  ->  overlay DB  ->  env proses asli

komentar di sumber: *"DB settings overlay (ADR-0019): beats the file, loses to
explicit process env"*. Jadi **berkas `.env` KALAH dari overlay DB**, dan nilai
kosong di DB = "tanpa override" (`override()` melewati nilai kosong). Itu
sebabnya `.env TLS_FINGERPRINT=bun` menang atas `config:TLS_FINGERPRINT=''` —
tapi kesimpulan lama "`.env` mengalahkan DB" hanya benar untuk kasus nilai DB
kosong. **Koreksi.**

Tindakan: baris `config:ACTING_USER_ID` dihapus (backup
`data/freebuff.db.bak-pre-acting`). Bukti: boot 15:45:19 masih mencatat
`acting user id set`, boot 15:51:57 tidak.

### Cacat protokol: `HTTP2_UPSTREAM=true` merusak ALPN persona `bun`

`stealth.Dialer` memanggil `setALPN(uConn, alpn)` (`stealth/tls.go:102`) yang
**mengganti** ekstensi ALPN milik spec in-place; `upstream/client.go` mengirim
`["h2","http/1.1"]` saat `HTTP2_UPSTREAM` aktif. Spec Bun memaku `["http/1.1"]`
(`bun_spec_test.go`), dan capture CLI live 0.0.194 juga satu entri
(`docs/operations/bun-1.3.14-clienthello.txt`, terlihat di record mentah
`0010000b000908 687474702f312e31`). Jadi `bun` + h2 = ClientHello yang cocok
dengan Bun **maupun** Chrome-tidak. JA3 tidak terpengaruh (ia meng-hash tipe
ekstensi), **JA4 membaca daftar ALPN**. Rasional knob ini (#51, "real browsers
advertise h2,http/1.1") khusus browser dan tidak berlaku untuk persona CLI.

VPS `vps-natusa` memang menjalankan `config:HTTP2_UPSTREAM=true` (di DB),
sementara `.env` sudah `TLS_FINGERPRINT=bun`. Perbaikan:

- baris DB `config:HTTP2_UPSTREAM` **dihapus**, nilai dipindah ke `.env` sebagai
  `HTTP2_UPSTREAM=false` — tier yang ditulis dashboard, jadi tidak ada lagi
  bayangan DB yang membuat suntingan dashboard tampak "tidak tersimpan".
- backup: `.env.bak-pre-http2`, `data/freebuff.db.bak-pre-http2`.
- bukti: boot log `applying 46 DB setting override(s)` (dari 47), healthz 200,
  `env_file=/opt/freebuff-proxy/.env`.

### Penjaga kode

- `config.CLIFaithfulProfile(name)` — satu sumber kebenaran untuk "profil ini
  mereproduksi ClientHello CLI sendiri" (kini hanya `bun`). `TLSPersonaWarning`
  dan `ALPNPersonaWarning` sama-sama memakai predikat ini, jadi capture baru
  yang ditambahkan otomatis ikut kedua aturan.
- `config.ALPNPersonaWarning(name, http2Upstream)` — memperingatkan `bun` + h2,
  memberi baris `ok` saat `false`, dan `("", false)` untuk profil non-CLI
  (preset browser memang menginginkan h2, plain Go tidak memaku ALPN).
- Dipakai di dua jalur: `cli_serve.go` (WARN setiap boot) dan `doctor.go`
  (`http2ALPNRow`, baris baru di `-doctor`).
- Test: `TestCLIFaithfulProfile`, `TestALPNPersonaWarning`, `TestHTTP2ALPNRow`.
- `bun_spec_test.go` tidak menangkap cacat ini karena ia menguji `bunSpec()`
  **sebelum** `setALPN` menyentuhnya — pin itu tetap benar dan tidak diubah.

### Verifikasi

- `gofmt -l backend/` kosong; `go build ./...` OK; `go vet ./...` bersih.
- `go test ./...` di `backend/` **hijau seluruh paket** (exit 0), termasuk e2e
  `cmd/freebucks-proxy` (53s) dan `internal/archtest`.
- VPS: `1.19.2.15`, `active`, healthz `status:ok`, override DB 47 → 46, tanpa
  baris `acting user id set`.

### Sisa / belum

- **Proses `freebuff.exe` pid 9332 masih hidup di host ini** dan tidak bisa
  dimatikan dari sesi agent (`Access is denied`, juga di luar sandbox). Guard
  menahan gateway, tapi login akun baru tetap butuh CLI ditutup manual.
- Penjaga ALPN baru **belum ter-deploy** ke VPS — binary `1.19.2.15` di sana
  dibangun sebelum perubahan ini, jadi peringatan boot-nya belum aktif.
- `UPSTREAM_EGRESS_URL` masih inert; opsi A/B/C/D masih menunggu pemilik repo.

## 2026-09-25 — temuan lanjutan: token banned di `.env` VPS + celah diagnostik `-doctor`

Saat memverifikasi baris ALPN baru lewat `-doctor` di VPS, muncul FAIL yang
tidak terduga:

    [FAIL] Token #1 validity probe failed: upstream account banned: {"status":"banned"}

Padahal `/healthz` melaporkan `mode:"bridge"`, `auth_tokens=0`. Penyebabnya:
`.env` VPS masih menyimpan **token yang sudah di-ban** (36 karakter), dan
**hanya** baris overlay DB `config:AUTH_TOKENS` (bernilai kosong; presence →
bridge mode + mematikan auto-discovery) yang mencegah service memakainya.

Bahaya laten: kalau baris DB itu hilang (dashboard menyimpan ulang, overlay
dibersihkan, DB dipulihkan dari snapshot lama), service langsung memakai token
banned → setiap request 403. `.env` sekarang menyatakan bridge mode secara
eksplisit: token dikomentari (nilai tersimpan di `.env.bak-pre-authtokens`)
dan `AUTH_TOKENS=` kosong ditulis apa adanya — persis bentuk yang ditulis
dashboard saat beralih ke bridge mode.

### Celah diagnostik: `-doctor` tidak menerapkan overlay DB

`Serve` memuat konfigurasi lewat `config.LoadOpts(..., LoadOptions{Overlay:
bootOverlay})` (`cli_serve.go:50-71`), tetapi `doctor.Run` memakai
`config.Load(configPath)` = `LoadOpts(path, LoadOptions{})` **tanpa overlay**.
Akibatnya `-doctor` dapat melaporkan konfigurasi yang TIDAK dipakai server —
di VPS ini ia menghasilkan FAIL palsu dengan memprobe token yang tidak pernah
disentuh service.

Ini juga sebabnya memindahkan `HTTP2_UPSTREAM` ke `.env` penting: `-doctor`
hanya melihat tier file/env, jadi nilai yang hidup **hanya** di DB tidak akan
terlihat olehnya.

**Belum diperbaiki** (butuh keputusan struktur paket): `-doctor` seharusnya
memuat overlay yang sama dengan `Serve`. Jalur bersih: satu helper bersama
yang membuka store lalu memanggil `config.OverlayFromRows`, dipakai `cli` dan
`cli/doctor` (aman — `cli` tidak mengimpor `cli/doctor`).

### Verifikasi akhir VPS (`vps-natusa`)

    binary   : 1.19.2.16 (sha256 32d555d1…, identik lokal↔VPS sebelum dipasang)
    systemd  : active
    healthz  : {"status":"ok","mode":"bridge","egress_region":"US"}
    -doctor  : 10 passed, 1 warning (bridge mode), 0 failed
               [ok] TLS_FINGERPRINT=bun
               [ok] HTTP2_UPSTREAM=false
    boot     : applying 46 DB setting override(s); auth_tokens=0 bridge_mode=true
    backup   : freebuff-proxy.bak-1.19.2.15-pre-alpn, .env.bak-pre-http2,
               .env.bak-pre-authtokens, data/freebuff.db.bak-pre-http2

## 2026-09-25 — `-doctor` kini memakai konfigurasi efektif yang sama dengan `Serve`

Celah yang dicatat di entri sebelumnya sudah diperbaiki. `Serve` membaca overlay
settings ADR-0019 sebelum `config.Load` pertama, sementara `doctor.Run` memanggil
`config.Load` langsung — jadi `-doctor` bisa melaporkan (dan memprobe)
konfigurasi yang tidak dipakai server. Teramati live: FAIL
`upstream account banned` terhadap token yang tidak pernah disentuh service,
sementara `/healthz` melaporkan bridge mode.

Wiring-nya sekarang hidup **sekali** di paket baru `internal/bootcfg`:

- `bootcfg.Open()` — membuka store (`store.OpenWithStatus(store.DBPathFromEnv())`),
  membaca `ListSettings()`, mengubahnya jadi overlay lewat
  `config.OverlayFromRows`, lalu mengembalikan handle + overlay + `MigrateStatus`
  + daftar `Notice{Warn, Msg}`. Setiap kegagalan non-fatal (jalan live-only).
- `bootcfg.Load(configPath, overlay)` — satu definisi "konfigurasi yang
  benar-benar dijalankan server": `config.LoadOpts` dengan
  `DiscoverCLIToken: clicreds.DiscoverToken` dan `Overlay`.

Dipakai `cli_serve.go` (Serve) dan `cli/doctor` (`Run` + `RunTokenTest`).
Duplikasi sengaja dihindari: menambah `LoadOption` hanya untuk Serve akan
membuka celah yang sama lagi.

**Catatan perilaku**: karena `DiscoverCLIToken` kini juga aktif di `-doctor`,
menjalankan `-doctor` di host yang CLI-nya login akan **memprobe token hasil
auto-discovery** (probe zero-cost, tanpa klaim sesi, lewat klien ber-persona
sama). Itu memang tujuannya — kalau CLI login ke akun yang di-ban, `-doctor`
kini mengatakannya alih-alih menyembunyikannya di balik "bridge mode".

### Matriks arsitektur diperluas (sengaja)

`internal/bootcfg` ditambahkan ke allowlist
`backend/internal/archtest/arch_test.go` (leaf deps: `clicreds`, `config`,
`store`), plus entri di `internal/cli` dan `internal/cli/doctor`. Tanpa itu
`TestBackendDependencyMatrix` merah.

### Test

`internal/bootcfg/bootcfg_test.go`:

- `TestLoadAppliesOverlayOverFile` — overlay DB mengalahkan `.env`.
- `TestLoadOverlayEmptiesAuthTokens` — kasus VPS: `.env` punya token, overlay
  `AUTH_TOKENS=` kosong → bridge mode (AUTH_TOKENS presence-sensitive).
- `TestSettingsCloseWithoutStore` — jalur live-only aman.

Test-nya hermetik: `AUTO_DISCOVER_TOKEN=false` dipasang supaya `Load` tidak
membaca `~/.config/manicode/credentials.json` milik mesin pengembang (teramati
mengisi `AUTH_TOKENS` dari login CLI nyata saat pertama dijalankan).

Verifikasi: `gofmt` bersih, `go build ./...` OK, `go vet ./...` bersih,
`go test ./...` hijau seluruh paket (termasuk e2e dan `archtest`).
