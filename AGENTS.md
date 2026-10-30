# AGENTS.md

Guidance for AI coding agents working in this repo. Human contributors will find it useful too. `README.md` covers user-facing behavior; this file covers how to change the code without breaking it.

## What this is

`clerify` is a single-user, local-first task and project manager CLI written in Go. All data lives in one JSON file (`store.json`). The only network call it ever makes is to the user-configured AI endpoint. It is a port of an earlier web app, so several behaviors deliberately mirror that app (see "Oracle parity" below).

- Module: `github.com/P-Baranwal/clerify`, Go 1.26.3
- Static binary: `CGO_ENABLED=0`, no runtime dependencies
- Direct deps: cobra, google/uuid, go-runewidth, ledongthuc/pdf, golang.org/x/sys

## Commands

```sh
make check            # go vet + go test ./... + gofmt -l check (run this before finishing)
make build            # -> ./bin/clerify
make fmt              # gofmt -w ./cmd ./internal
go test ./...
go test -race ./...   # needed when touching internal/store
go test ./internal/cli -run TestName -v
go mod tidy           # CI fails if this changes go.mod/go.sum
```

CI (`.github/workflows/ci.yml`) runs: gofmt check, `go mod tidy` + `git diff --exit-code`, `go vet`, `go test`, and a `CGO_ENABLED=0` build. Match all of these locally.

## Layout

```
cmd/clerify          entry point (just calls cli.Execute)
internal/model       Item/Project/Profile/Store types, constants, Normalize*/Parse* functions, dates
internal/capture     offline natural-language capture ("essay due friday, about 5h")
internal/store       atomic JSON persistence, locking, journal, migrations, legacy web-app import
internal/ai          provider client (two wire formats), presets, prompts, permissive parsers
internal/derive      pure functions: ranking, ROI, risk, analytics, calendar, kanban
internal/render      terminal output: tables, calendar, plan, report, board
internal/cli         cobra commands; App struct holds process state
internal/config      config.json (AI settings + UI prefs), separate from the store
internal/pdf         PDF/text extraction (pdftotext, then pure-Go fallback)
internal/format      shared number/hours formatting
internal/atomicfile  temp file + fsync + rename helper
testdata/fakeai.py   local fake OpenAI-style server on 127.0.0.1:8765 for manual testing
```

Dependency direction: `model` and `format` are leaves; `store`, `derive`, `ai`, `render` depend on `model`; `cli` depends on everything. Keep `derive` and `render` free of I/O beyond the `io.Writer` they are handed.

## Rules that are easy to get wrong

### Writes always go through `db.Mutate`

- Never modify `db.Data` directly and never write `store.json` yourself. `Mutate` takes the lock, reloads from disk, runs your function, normalizes, backs up, writes atomically, and appends journal entries, all in one critical section. That is what stops two terminals from clobbering each other.
- `db.Data` is a snapshot taken at `Open` (refreshed after each `Mutate`). It is fine for reads and for resolving ids, but inside a `Mutate` callback use `tx.Item` / `tx.Project` / `tx.Store()` and handle `store.ErrNotFound` there, because the item may have vanished between resolution and the lock.
- Convenience methods (`AddItem`, `UpdateItem`, ...) exist for simple cases. Commands that need a journal entry use `Mutate` directly so the snapshot and the entry share the lock.
- All file writes use `atomicfile.WriteFile`. `config.json` is `0600` and holds the API key; the key must never be written to `store.json` or the journal.

### Every undoable mutation must journal

- Call `tx.Journal(store.Entry{...})` inside the same `Mutate`. Take `before := cur.Clone()` before changing anything, and snapshot `saved := cur.Clone()` after.
- If you add a new kind of mutation, add the matching case to `undoEntry` in `internal/cli/mutate.go` and a test that does mutation, then `undo`, then asserts the original state.
- `prepareEntry` strips `RawContent` from update/rescore/create snapshots to keep the journal small (delete entries keep it). `restoreItem` compensates by preserving the current `RawContent`. If you change either side, keep both in sync (`TestUndoUpdatePreservesRawContent`).
- The journal is capped at `MaxJournalEntries` (50).

### Normalization and strict parsing are two layers

- `model.Normalize*` is forgiving and applied to stored data (clamps, fills defaults, maps legacy values). `model.Parse*` (`ParseStatus`, `ParseType`, `ParsePreset`, `ParseTheme`, `ParseDeadlineStrict`, `ParseDeadlineInput`, `ValidateHours`) is strict and belongs at the CLI boundary so typos exit with code 2 rather than silently becoming defaults.
- When `ImpactScore` is set, `NormalizeItem` recomputes `PriorityScore` from ROI (`impact / max(hours, 0.5) * 10`, clamped) and overwrites the reasoning. This is why `edit --priority` errors if an impact score is present. Do not fight this in individual commands.
- `Status` accepts the legacy alias `active` -> `todo` and the table word `doing` -> `in_progress`. `CompletedAt` is only kept while status is `done`.

### Dates and times

- Deadlines are `YYYY-MM-DD` strings parsed as local midnight via `ParseInLocation`. Never use `time.Parse` for deadlines; it returns UTC and brings back the off-by-one-day bug.
- `ParseDeadlineStrict` is for stored/CSV data and accepts only `YYYY-MM-DD`. `ParseDeadlineInput(s, now)` is the friendly CLI boundary for `--due`: it also accepts `today`, `tomorrow`, weekdays, `+3d`, `next week`, and month/day forms, then canonicalizes to `YYYY-MM-DD`. Use it in `add` and `edit`; keep imports strict.
- Stored timestamps are RFC3339 UTC via `model.NowRFC3339`. Display goes through `render.FormatTimestamp`, which honors the configured `DateFormat`.
- `CompletedAt` is separate from `UpdatedAt`. Velocity, streaks, and project breakdowns must use `Item.CompletionTime()`, not `UpdatedAt`, so editing a done item does not move it into a new week.
- Use the injectable clocks (`a.now()`, `tx.Now()`, `db.Now`) in command logic, not `time.Now()`, so tests can pin time. `derive` functions take `now` as a parameter; keep it that way.

### Onboarding, references, and friendly input

- Bare `clerify` is not help. `rootCmd` runs `today` after setup, or a three-line welcome on a fresh store (`App.Fresh`, set in `ensure` before `store.Open`). `--json` always takes the `today` path.
- `clerify init` is the onboarding wizard. It only writes `config.json`; `--no-ai` leaves everything untouched. Hidden key input uses `stty -echo` on unix; on Windows the input is visible. Non-interactive `init` prints the three `ai` commands instead of prompting.
- `clerify add` with no text prompts for a title and optional due date when stdin is a terminal, and keeps failing with exit 2 otherwise.
- `--due` parsing goes through `model.ParseDeadlineInput`. Keep the strict parser for stored data.
- Quick capture lives in `internal/capture`. It is conservative: a title with no date/effort cue is left alone. `runAdd` applies capture only when no explicit title/due/est/type flag was passed, and explicitly set flags always win. AI extraction (`BuildExtractTaskPrompt` + `ParseExtractedTask`) runs only when capture matched, AI is not disabled, and a key/local `no_auth` preset exists; failures warn and fall back to the offline result.
- Item references are resolved by `resolveItemRef` in `internal/cli/refs.go`: full id, unique id prefix, `#N`, bare row number if no id matched, exact title, then unique title substring. `referenceItems` is the stable order of open items by priority; `referenceNumbers` is what `ls` shows. Completed items have no number and are addressed by id or title. Keep `#N` consistent with that order.
- Tables show `PriorityLabel` (`High` >= 70, `Medium` >= 40, otherwise `Low`) instead of the raw score. `show` and `--json` keep the number. `ItemTable` hides `IMPACT` and `PROJECT` when no row uses them.
- Commands are grouped for help via `addGrouped` (`groupEveryday`, `groupAdvanced`). New commands need an `Example` and belong in a group.

### Output conventions (CLI + render)

- Data goes to stdout; warnings, progress, and notes go to stderr (`a.Err`). With `--json`, stdout must be exactly one valid JSON document (there are tests for this, e.g. `TestPlanRefreshJSONIsOnlyJSON`).
- `--quiet` silences `a.printf` and warnings but never errors. `--dry-run` must not write the store and must not call the AI; AI commands print the system and user prompts instead.
- Never measure column widths with `len()`. Use `render.Width` / `render.PadRight`, which handle combining marks (Devanagari), CJK width, and strip ANSI codes. Color goes through `colorize` and only emits escapes when `Options.Color` is true.
- `render.Truncate` cuts by runes, not columns. That is acceptable for the current call sites; do not rely on it for width-exact layout.

### Exit codes

Use `fail(code, err)` and the helpers in `root.go` (`exactArgs`, `minArgs`, `notFound`).

| Code | Meaning |
|---|---|
| 0 | ok |
| 1 | runtime error, aborted confirmation, nothing to undo |
| 2 | usage / invalid input |
| 3 | not found or ambiguous id |
| 4 | AI, config, or parse error |
| 5 | store unreadable |
| 6 | lock timeout |

`codeFor` maps typed errors to codes. New error types that should have a specific code need a branch there.

## Adding a command

1. Add a `func (a *App) fooCmd() *cobra.Command` in the appropriate `internal/cli/*.go` file.
2. Call `a.ensure()` first in `RunE` (it lazily opens the store and config).
3. Register it in `rootCmd()` in `internal/cli/root.go` with `addGrouped(root, groupEveryday|groupAdvanced, ...)`.
4. Give it an `Example` and, where it takes an item/project/status argument, a `ValidArgsFunction` or `RegisterFlagCompletionFunc` (see `internal/cli/completion.go`).
5. Support `--json` using `a.printJSON`, and respect `--dry-run` / `--quiet` where relevant.
6. Add a test in `internal/cli` and update the command table in `README.md`.

## AI layer

- One client, two wire formats (`openai`, `anthropic`). Vendor specifics live only in the preset table in `internal/ai/config.go`. Do not add per-vendor code paths elsewhere.
- `DefaultConfig` is OpenAI. `clerify init` also defaults to OpenAI; Sarvam is still available but no longer the default. Existing `config.json` files are untouched.
- Applying a preset must not carry the previous provider's key or `extra_body` over (`TestPresetSwitchClearsOldKey`, `TestApplyPresetsDoNotInheritSarvamExtraBody`). Key resolution order: `--api-key`, `CLERIFY_API_KEY`, `<PRESET>_API_KEY`, `config.json`.
- Anything printed from config must go through `maskKey` / `redactAIConfig` (extra headers can hold credentials).
- Missing-key/base-URL/model errors point at `clerify init`. Keep the phrase "no API key" for the existing test.
- Untrusted text (task content, briefs) is JSON-encoded with `json.Marshal` inside prompts so it cannot close the prompt block. Keep doing that for any new prompt. Prompt wording is audience-neutral; keep the load-bearing heuristics and update `internal/ai/parse_test.go` when you change wording.
- The prompts in `internal/ai/prompts.go` carry the tuned heuristics (time anchors, difficulty scale, urgency bands). Change them deliberately and update the assertions in `internal/ai/parse_test.go` in the same change.
- Parsers in `parse.go` are intentionally permissive: they strip fences, take first-bracket to last-bracket, coerce strings to numbers, and treat 0 as "unset" for difficulty, priority, and hours (mirroring `parseInt(x) || default`). Do not "fix" that. Deadlines are the exception: a bad deadline is rejected.
- `analyze --re` and `rescore` refresh only AI-owned fields (difficulty, priority, reasoning) and leave user-edited title, type, deadline, and estimate alone. Plain `analyze` may fill empty fields.

## Store, schema, and import/export

- Current schema is `model.SchemaVersion = 2`. To change the on-disk shape, bump the constant and add a step to `migrate` in `internal/store/migrate.go`. A store with a newer schema than the binary must be rejected, not downgraded.
- Corrupt `store.json` recovers from `store.json.bak`, quarantines the bad file as `store.json.corrupt-<timestamp>`, and must not overwrite the good backup on the first post-recovery save (`skipBackupOnce`).
- Locking is `flock` on unix, `LockFileEx` on Windows, and a no-op elsewhere (build-tagged files in `internal/store`). Keep the three in step.
- CSV export prefixes cells starting with `=`, `+`, `-`, `@` (and tab/CR) with `'` to prevent formula injection; import strips it. The column order is `csvHeader` in `report.go`. A new item field that users can see should round-trip (`TestCSVRoundTripAllFields`).
- Import semantics: replace mode swaps items and projects (CSV replace leaves projects alone, since a CSV has none); `--merge` uses `UpsertItemsIfNewer`, which never overwrites a locally newer item.
- Legacy web-app exports are detected by `_exportVersion` and converted by `store.ParseLegacy`. Keep that path tolerant of malformed or duplicate records and report problems as warnings.

## Testing conventions

- CLI tests use `runCLI(t, stdin, args...)` with `--data-dir t.TempDir()`. They exercise the real command tree and assert on exit codes, stdout, and stderr.
- Tests must never hit the network or read the developer's real config. For AI behavior, use `httptest` (`fakeAI`, `failingAI`, `writeAIConfig`) and call `clearKeyEnv(t)` so ambient API keys do not leak in.
- `derive` and `model` tests pin `now` with `time.Date(..., time.Local)`. Prefer fixed dates over `time.Now()`.
- `internal/model/dates_test.go` covers friendly deadlines; `internal/capture` covers offline capture; `internal/cli/onboarding_test.go` covers first-run, `init`, `guide`, doing, `#N`/title references, headers, and empty states.
- Store changes that touch locking or concurrency need a test that passes under `go test -race`.
- Table-driven tests are the norm for parsers and normalizers.

## Style

- Standard `gofmt`; CI enforces it. Doc comments on exported identifiers, written as full sentences.
- Wrap errors with `%w` and include the failing path or value. User-facing error messages are lowercase, with no trailing period.
- Prefer small pure functions in `derive`; keep side effects in `cli` and `store`.
- No new dependencies without a clear need; the binary is meant to stay small and static.

## Known gaps

- There is no interactive TUI; the `tui` command was removed rather than advertised as a stub.
- Hidden API-key input uses `stty` on unix. On Windows the key is visible while typed.
- `pdftotext` is optional. Without it, PDF import uses the pure-Go extractor, which handles fewer layouts and can't read scanned PDFs.