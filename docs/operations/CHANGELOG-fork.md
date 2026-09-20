# Changelog (fork jhopan/freebuff-proxy)

Catatan perubahan pada fork ini di atas upstream `trefeon/freebuff-proxy`.
Rilis resmi tetap mengikuti upstream; hanya deviasi fork yang dicatat di sini.

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
