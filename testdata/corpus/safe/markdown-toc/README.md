# markdown-toc

Generate a Table of Contents from a Markdown document by scanning its `#` headings.

## What it does
- Parses the input string for `^#{1,6}\s+(.+)$` lines
- Builds a nested bullet list with anchor links

## What it does NOT do
- No filesystem reads outside the input string
- No network
- No shell
