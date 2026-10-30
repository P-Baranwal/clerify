# Exit codes

| Code | Meaning |
|---|---|
| `0` | ok |
| `1` | runtime error, aborted confirmation, or nothing to undo |
| `2` | usage or invalid input |
| `3` | item or project not found, or an ambiguous reference |
| `4` | AI, configuration, or AI response parse error |
| `5` | store unreadable |
| `6` | lock timeout |

## References

Item references may be a full id, a unique id prefix, a `#N` row number, or a
title. Projects may be a full id, a unique id prefix, or an exact title.

If a prefix or title matches more than one record, clerify exits with code `3`
and lists the short ids and titles. At a terminal it will offer a picker instead
of failing.

## Data goes to stdout, notes go to stderr

With `--json`, stdout is exactly one valid JSON document. Warnings, progress,
and notes go to stderr. `--quiet` suppresses non-essential output but never
errors. `--dry-run` never writes the store and never calls the AI.
