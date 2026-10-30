# clerify

A personal, local-first task and project manager for the terminal. No account,
no server, no database. Everything lives in one JSON file you own, and the only
network call it ever makes is to the AI endpoint you configure—and AI is
optional.

## 60-second quickstart

Requires Go 1.26+. The binary is static (`CGO_ENABLED=0`) and has no runtime
dependencies. `pdftotext` (poppler-utils) is optional but improves PDF import.

```sh
git clone https://github.com/P-Baranwal/clerify && cd clerify
make build            # -> ./bin/clerify
# or
go install github.com/P-Baranwal/clerify/cmd/clerify@latest
```

```sh
clerify init                              # optional AI setup; answer "no" to skip
clerify add "Write report" --due friday
clerify today
clerify done #1
clerify undo
```

That is the whole loop. `init` only configures AI scoring; capture, listing,
deadlines, projects, reports, and undo all work without it. The four AI-only
commands are `analyze`, `rescore`, `plan --refresh`, and `wbs`.

## Daily workflow

1. **Capture.** `clerify add "Write report" --due friday --est 3`. Short
   natural-language lines work too: `clerify add "essay due friday, about 5h"`.
2. **See what's next.** `clerify today` shows overdue items, what is due today,
   and the top five priorities. `clerify ls` shows everything with a stable
   `#N` reference.
3. **Pick something.** `clerify start #1` or `clerify show #1`.
4. **Finish it.** `clerify done #1 --actual 4`.
5. **Fix a mistake.** `clerify undo`.

Run `clerify guide` any time for the same walkthrough in the terminal.

## Friendly input

- **Deadlines** accept `YYYY-MM-DD`, `today`, `tomorrow`, weekday names (`fri`),
  relative days (`+3d`, `in 3 days`), `next week`, and month/day forms such as
  `Nov 2`. Deadlines are stored as `YYYY-MM-DD`.
- **References** can be a short id prefix (`7f3a1b`), a row number (`#1`), or a
  title (`clerify done "read paper"`). If a title matches several items,
  clerify lists their short ids and lets you pick at a terminal.
- **Quick capture** pulls a deadline, estimate, and type out of a short line.
  Explicit flags always win, and plain titles with no date or effort cue are
  left untouched.
- **Priority labels** are shown as `High`, `Medium`, or `Low` in tables; the
  number stays available in `show` and `--json`.
- **Shell completion** suggests item ids, project ids, and statuses. Enable it
  with `clerify completion bash|zsh|fish|powershell`.

Every command accepts `--json` for scripting, `--no-color` to disable ANSI
output, `--quiet` to suppress non-essential output, and `--dry-run` for
anything that would call the AI or write.

## Commands

### Everyday

| Command | What it does |
|---|---|
| `add [text]` | create an item; short natural-language capture is automatic |
| `ls [query]` | list items with `#N`, full column names, and priority labels |
| `show <id>` | full record, checklist, boost, reasoning |
| `edit <id>` | change any field; checklist ops via `--check-add/--check-rm/--check-done/--check-undone` |
| `todo/start/block/unblock/done <id>` | status transitions (`block --note`, `done --actual`) |
| `rm <id>...` | delete; undoable |
| `undo` | reverse the most recent un-undone mutation (`--list` to inspect) |
| `today` | overdue, due today, top 5; `--all` shows every section |
| `plan [--refresh]` | weekly plan, persisted between runs |
| `calendar [--month YYYY-MM]` | month grid with deadline chips |
| `project new/ls/show/done/rm` | projects; `project show --board` groups items by status |
| `digest [--week]` | one-screen summary for cron |
| `init` | set up AI in a few questions (`--no-ai` to skip) |
| `guide` | short built-in tutorial |

### Advanced

| Command | What it does |
|---|---|
| `analyze [id\|file\|-]` | run AI analysis on an item or new text |
| `boost <id> --reason "..."` | temporarily raise priority (`--clear` to drop it) |
| `rescore` | re-run AI scoring in bulk (alias: `refresh-scores`) |
| `wbs [project] --apply` | break a brief into tasks (alias: `breakdown`) |
| `report [section...]` | pace, estimate accuracy, projects, streaks, calibration (`--last`, `--md`) |
| `export [--json\|--csv\|--ics]` | dump the store, a lossless item CSV, or an iCalendar feed |
| `import <file> [--merge] [--from-csv]` | restore a store or a CSV |
| `config show/set/path` | profile and file locations |
| `ai preset/key/test/models/show` | provider setup and diagnostics |
| `doctor` | diagnose the local setup |

Run `clerify <command> --help` for flags and examples. Exit codes are listed in
[docs/exit-codes.md](docs/exit-codes.md).

## How scoring works

A new item starts with a neutral priority of 50 and a 1-hour estimate. `clerify
analyze <id>` asks the AI to fill in difficulty, effort, priority, reasoning,
and a checklist. Tables turn the 0-100 score into `High` (70+), `Medium`
(40-69), or `Low` (below 40).

Setting `--impact` switches that item to ROI scoring: priority becomes
`impact ÷ max(hours, 0.5) × 10`. The number is explained in the output, and
`show` keeps the reasoning. Because ROI owns the priority, `edit --priority` is
rejected while an impact score is set.

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

`clerify init` is the easy path: it asks whether you want AI, lists common
providers, hides the key while you paste it, and runs a test call. The default
provider is OpenAI; no key is required until you choose a hosted provider.

Provider endpoints, key resolution, custom endpoints, and quirks are documented
in [docs/providers.md](docs/providers.md).

Anything OpenAI-compatible also works without a preset:

```sh
clerify ai preset custom --base-url http://localhost:11434/v1 --model qwen2.5-coder:7b
clerify ai preset custom --base-url https://my-gateway.example/v1 --header x-api-key --prefix "" --wire openai
```

`clerify config show` prints the effective settings with the key masked.

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
internal/capture   offline natural-language capture
internal/store     atomic JSON persistence, locking, journal, legacy import
internal/ai        generic provider client, prompts, permissive parsers
internal/derive    ranking, ROI, risk, analytics, calendar
internal/render    tables, calendar grid, plan and report output
internal/cli       cobra commands
internal/pdf       pdftotext + pure-Go fallback
internal/config    config.json (AI settings + UI prefs)
internal/format    shared number/duration formatting
internal/atomicfile atomic temp-file + rename helper
```

The AI prompts in `internal/ai/prompts.go` are the valuable asset—they carry
every heuristic (time anchors, difficulty scale, urgency bands). If you change
them, update the snapshot assertions in `internal/ai/parse_test.go` deliberately.
Untrusted task/brief text is JSON-encoded in the user message so it cannot close
the prompt block.
