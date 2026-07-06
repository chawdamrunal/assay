# readonly-stats

Reports stats (line count, byte size) for files explicitly passed to it.

## What it does
- Takes a list of relative file paths from its input
- Stats each file and returns size + line count

## What it does NOT do
- Does not read file contents (only metadata + a line-count read)
- No path traversal — rejects absolute paths and `..`
- No network, no shell
