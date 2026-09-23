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
