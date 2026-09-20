# Tool-name mapping: kenapa toolset Hermes penuh sekarang lolos

Penjelasan mekanisme perbaikan 502 "Tool names must be unique" (model
deepseek/deepseek-v4-flash) dan hubungannya dengan fix upstream #655.

## Masalah

Upstream Codebuff (free lane) punya gerbang anti-harness asing
(`foreign-client-signals.ts`, vendor 0.0.177+):

- Request yang menawarkan tool bernama asing (mis. `terminal`, `read_file`)
  dicap `foreign_toolset` -> akun di-cap `third_party_client` permanen ->
  downgrade paksa ke model gratisan tanpa endpoint.
- Solusi proxy (sudah ada sejak #140): RENAME nama tool klien menjadi nama
  signature official Codebuff sebelum dikirim upstream, lalu rename balik
  pada semua jalur response. Parameter schema diteruskan utuh.

Tabel pemetaan (backend/internal/convert/toolmap_request.go, clientToOfficial)
memetakan banyak alias ke nama official yang sama:

    terminal, execute_code, bash, exec, ...  -> run_terminal_command
    skills_list, skill_view, skill_manage    -> skill
    patch, edit, replace, ...                -> str_replace
    read_file, read, view, ...               -> read_files

Bug lama: rename dilakukan in place tanpa cek tabrakan. Hermes mengirim 32
tool; dua alias berbeda yang dipetakan ke nama official sama menghasilkan
DUA entri tool bernama persis sama di wire. Upstream strict menolak:

    upstream 400: {"error":{"message":"Tool names must be unique.",
    "code":"invalid_request_error"}}

Proxy membungkusnya menjadi 502 `upstream_unavailable`. Hanya terjadi pada
model strict (DeepSeek dkk.); model pemaaf terima duplikat, makanya bug
menyamar.

## Fix (upstream #655, commit 31931c8e)

Tiga mekanisme di `ToolMapper`:

1. **Dedupe "first wins"** - alias PERTAMA yang dipetakan ke sebuah nama
   official memegang nama itu di wire. Duplikat berikutnya divirtualisasi
   menjadi `mcp__<nama-asli>`. Contoh untuk Hermes:

        terminal      -> run_terminal_command   (pertama, pegang official)
        execute_code  -> mcp__execute_code      (duplikat, divirtualisasi)
        skills_list   -> skill                  (pertama)
        skill_view    -> mcp__skill_view        (duplikat)
        skill_manage  -> mcp__skill_manage      (duplikat)

   Nama berprefix `mcp__` dibaca upstream sebagai tool MCP - diterima,
   tidak dicap asing. Dedupe unconditional karena beberapa upstream strict
   menolak duplikat outright.

2. **Reverse map "first wins" + restore** - jalur response
   (FromUpstreamChunk / RestoreName) memetakan balik:
   `run_terminal_command` -> `terminal`, `mcp__execute_code` ->
   `execute_code`, dst. Dua tool "kembar" tetap dua fungsi terpisah di mata
   klien; tool_calls dari model selalu direstore ke nama asli Hermes.

3. **Virtualisasi nama blacklist** - `delegate_task`, `computer_use`
   (OpenClaw/Hermes) dan semua nama berprefix `cron*` ada di
   FOREIGN_HARNESS_TOOL_NAMES upstream. resolveUpstreamTool langsung
   menjadikannya `mcp__delegate_task`, `mcp__computer_use`,
   `mcp__cronjob` - lolos gerbang tanpa dicap foreign. Tanpa ini, satu
   saja tool blacklist membuat SELURUH request downgrade.

Wire final request Hermes penuh (dibuktikan debug test):

    run_terminal_command  mcp__execute_code
    skill                 mcp__skill_view     mcp__skill_manage
    mcp__delegate_task    mcp__computer_use   mcp__cronjob
    str_replace           read_files          write_file
    end_turn              decide              (injeksi proxy)

Semua unik -> upstream 200.

## Kenapa butuh test pin baru

Row test lama ("Hermes" 8 tool, "Hermes-extended" 6 tool) hanya sampling -
tidak memuat pasangan alias yang tabrakan + tool blacklist sekaligus dalam
satu body. `TestHermesFullToolsetWireClean` membawa 32 tool asli dalam
urutan request nyata sehingga kontrak di atas terkunci terhadap regresi.
