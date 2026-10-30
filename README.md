# clerify

A personal, local-first task and project manager for the terminal.

No account, no server, no database. Everything lives in one JSON file you own,
and the only network call the tool ever makes is to the AI endpoint you
configure.

Untrusted assignment/brief text is JSON-encoded in the user message so it cannot
close the prompt block.

## Install

```sh
git clone https://github.com/P-Baranwal/clerify && cd clerify
make build            # -> ./bin/clerify
# or
go install github.com/P-Baranwal/clerify/cmd/clerify@latest
```

Requires Go 1.26+. The binary is static (`CGO_ENABLED=0`) and has no runtime
dependencies. `pdftotext` (poppler-utils) is optional but improves PDF import.

## Quick start

```sh
clerify ai preset sarvam                       # or openai, groq, ollama, ...
printf '%s' "$SARVAM_API_KEY" | clerify ai key --stdin
clerify ai test

clerify add "5-page research essay, due Nov 2" --type Essay --due 2026-11-02 --analyze
clerify ls
clerify today
clerify plan --refresh
clerify done 7f3a1b --actual 9
clerify undo
```

Every command accepts `--json` for scripting, `--no-color` to disable ANSI
output, and `--dry-run` for anything that would call the AI.

## Data

Resolution order: `--data-dir`, then `$CLERIFY_HOME`, then your platform config
directory (`~/.config/clerify` on Linux).

| File | Purpose | Mode |
|---|---|---|
| `store.json` | items, projects, profile, cached plan | `0644` |
| `config.json` | AI provider settings and API key | `0600` |
| `journal.jsonl` | last 50 journal entries, used by `undo` | `0600` |
| `store.json.bak` | previous store, written on every save | `0644` |

Writes are atomic (temp file + fsync + rename) and transactional: the store lock
covers load, mutation, save, and the journal append, so two terminals cannot
silently overwrite each other. The schema is version 2; `CompletedAt` is stored
separately from `UpdatedAt`, and older stores are migrated on open. If
`store.json` is ever corrupt, `clerify` recovers from `store.json.bak`, saves the
corrupt file as `store.json.corrupt-<timestamp>`, and tells you.

## AI providers

There is one client with two wire formats, so any key works. Presets only
pre-fill the base URL, auth header, and default model:

| Preset | Endpoint | Auth |
|---|---|---|
| `sarvam` | `https://api.sarvam.ai/v1` | `api-subscription-key` |
| `openai` | `https://api.openai.com/v1` | `Authorization: Bearer` |
| `groq` | `https://api.groq.com/openai/v1` | `Authorization: Bearer` |
| `openrouter` | `https://openrouter.ai/api/v1` | `Authorization: Bearer` |
| `together` | `https://api.together.xyz/v1` | `Authorization: Bearer` |
| `deepseek` | `https://api.deepseek.com/v1` | `Authorization: Bearer` |
| `mistral` | `https://api.mistral.ai/v1` | `Authorization: Bearer` |
| `xai` | `https://api.x.ai/v1` | `Authorization: Bearer` |
| `gemini` | `https://generativelanguage.googleapis.com/v1beta/openai` | `Authorization: Bearer` |
| `ollama` | `http://localhost:11434/v1` | ignored |
| `lmstudio` | `http://localhost:1234/v1` | ignored |
| `llamacpp` | `http://localhost:8080/v1` | ignored |
| `vllm` | `http://localhost:8000/v1` | ignored |
| `anthropic` | `https://api.anthropic.com/v1` | `x-api-key`, `wire: anthropic` |
| `custom` | you supply | you supply |

Keys are resolved in this order: `--api-key`, `CLERIFY_API_KEY`,
`<PRESET>_API_KEY`, then `config.json`. The four local presets set `no_auth`,
so they work without a key and send no authentication header.

Switching presets clears the previous provider's key, so an OpenAI key is never
sent to DeepSeek's endpoint. Provider-specific body fields such as Sarvam's
`reasoning_effort` are applied only by that preset and can be removed by
editing `config.json`.

Anything OpenAI-compatible works without a preset:

```sh
clerify ai preset custom --base-url http://localhost:11434/v1 --model qwen2.5-coder:7b
clerify ai preset custom --base-url https://my-gateway.example/v1 --header x-api-key --prefix "" --wire openai
```

Provider quirks (extra body fields, extra headers, JSON mode) are configurable:

```sh
$EDITOR ~/.config/clerify/config.json   # ai.extra_body, ai.extra_headers, ai.response_format_json
```

`clerify config show` prints the effective settings with the key masked.

## Commands

| Command | What it does |
|---|---|
| `add [text]` | create an item; `--analyze` fills it with AI |
| `analyze [id\|file\|-]` | run analysis on an item or new text |
| `ls [query]` | list and filter (`--status`, `--project`, `--overdue`, `--due-within`, `--type`, `--sort`, `--all`, `--done`, `--limit`) |
| `show <id>` | full record, checklist, boost, reasoning |
| `edit <id>` | change any field; checklist ops via `--check-add/--check-rm/--check-done/--check-undone` |
| `todo/start/block/unblock/done <id>` | status transitions (`block --note`, `done --actual`) |
| `rm <id>...` | delete; undoable |
| `undo` | reverse the most recent un-undone mutation (`--list` to inspect) |
| `boost <id> --reason "..."` | temporary priority bump (`--clear` to drop it) |
| `rescore` | re-run AI scoring in bulk; `--include-empty` opts title-only items in |
| `today` | overdue, due today, top 5, inbox, high ROI, capacity risk, today's plan |
| `plan [--refresh]` | weekly plan, persisted between runs |
| `calendar [--month YYYY-MM]` | month grid with deadline chips |
| `project new/ls/show/done/rm` | projects; `project show --board` groups items by status |
| `wbs [project] --apply` | generate a work breakdown and create the items |
| `report [section...]` | velocity, accuracy, projects, streaks, calibration, insights (`--last`, `--md`) |
| `digest [--week]` | one-screen summary for cron |
| `export [--json\|--csv\|--ics]` | dump the store, a lossless item CSV, or an iCalendar feed |
| `import <file> [--merge] [--from-csv]` | restore a store or a CSV |
| `config show/set/path` | profile and file locations |
| `ai preset/key/test/models/show` | provider setup |
| `doctor` | diagnose the local setup |
| `tui` | interactive UI (not implemented yet) |

Exit codes: `0` ok, `1` runtime error, `2` usage, `3` not found / ambiguous id,
`4` AI or configuration error, `5` unreadable store, `6` lock timeout.

Ids may be abbreviated to any unique prefix.

## CSV round-trips

`export --csv` writes every user-visible item field: id, title, type, status,
priority, estimate/actual hours, deadline, timestamps, impact, difficulty,
notes, description, project id, checklist (including done state), analysis and
blocker metadata, boost fields, and raw content. Cells beginning with `=`, `+`,
`-`, or `@` are prefixed with `'` to prevent spreadsheet formula injection; the
prefix is removed on import.

`import <file> --from-csv` treats a CSV as item data. In replace mode it leaves
the existing project collection alone, because a CSV has no project list.
`--merge` keeps a local item when its `updatedAt` is newer than the incoming
one, so restoring an older backup cannot clobber recent edits.

## Development

```sh
make check          # gofmt check, vet, tests
make build
go test -race ./... # store concurrency coverage
go test ./... -run TestParseLegacyExport -v
go mod tidy
```

Layout:

```
cmd/clerify        entry point
internal/model     item/project/profile types, normalization, dates
internal/store     atomic JSON persistence, locking, journal, legacy import
internal/ai        generic provider client, prompts, permissive parsers
internal/derive    ranking, ROI, risk, analytics, calendar
internal/render    tables, calendar grid, plan and report output
internal/cli       cobra commands
internal/pdf       pdftotext + pure-Go fallback
internal/format     shared number/duration formatting
internal/atomicfile atomic temp-file + rename helper
```

The AI prompts in `internal/ai/prompts.go` are the valuable asset — they carry
every heuristic (time anchors, difficulty scale, urgency bands). If you change
them, update the snapshot assertions in `internal/ai/parse_test.go` deliberately.
