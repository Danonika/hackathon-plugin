# Document Format Matrix

Per-format behaviour of `documents.extract`, `documents.ingest` and
`documents.get`. All paths are relative to `data/input`.

## Routing

| Extension | Processor | Output shape |
|---|---|---|
| `txt`, `md`, `json`, `log`, `yaml`, `yml`, `xml`, `html`, `htm`, `srt`, `vtt` | Raw text read | File content, trimmed of a trailing partial rune |
| `csv`, `tsv` | Raw text read | Delimited text, one row per line |
| `docx` | `word/document.xml` walk | Paragraph text; `w:tab` -> tab, `w:br` -> newline, paragraph end -> newline |
| `xlsx` | All worksheets | `## Sheet: <name>` then tab-joined header + up to 500 rows per sheet |
| `pdf` | Text layer | Extracted text, or empty text plus an OCR warning |
| `xls` | None | Error: legacy `.xls` (BIFF) is not supported |
| Anything else | Raw fallback | Raw bytes decoded as text plus a no-processor warning |

## Limits and Warnings

| Limit | Value | Behaviour when hit |
|---|---|---|
| `documents.extract.max_chars` | 60000 default; `<= 0` or `> 10_000_000` uses the configured default | `text` is cut to `max_chars`, `chars` still reports the full extracted length, `truncated: true` plus warning `text truncated; use documents.ingest and knowledge.index for large documents` |
| PDF extraction | 10,000,000 characters | Warning `pdf text truncated at 10000000 bytes` |
| Raw fallback read | 20 MB | Warning `file is larger than 20MB; only the first 20MB was read` |
| XLSX text rendering | 500 rows per sheet | Inline marker `... N more rows omitted` in the text |
| `documents.get` | `max_chars` default as above; `offset` default 0, negatives clamp to 0 | `chars` is the returned slice length, `total_chars` the artifact length, `truncated: true` when `offset + chars < total_chars` |

`documents.ingest` stores the full extracted text as
`extracted/<slug>-<hash>-<timestamp>.txt` plus a `.meta.json` sidecar, and
returns `artifact_id`, `chars` and a 400-character `preview`. There is no
truncation on ingest; truncation happens only when reading back with
`documents.get`.

## Choosing extract vs ingest

| Situation | Call |
|---|---|
| One question, small file | `documents.extract` |
| Text will be read again or paged | `documents.ingest`, then `documents.get` |
| Text will be searched by passage | `documents.ingest` -> `knowledge.index` -> `knowledge.search` |
| XLSX that needs statistics | `$data-analysis` (`data.inspect` / `data.query`); extraction flattens the grid |
| Scanned PDF | No path; report OCR is required |

## PDF Notes

- Only an embedded text layer is extracted. An image-only scan returns
  `text: ""` and the warning `pdf contains no extractable text (possibly a
  scanned document); OCR is required`.
- Extraction failures return a warning `pdf text extraction failed: <error>` and
  empty text rather than an error status.
- Page numbers are not preserved; cite an offset from `documents.get` instead.

## XLSX Notes

- Every worksheet is included in workbook order; the sheet name appears as a
  literal `## Sheet: <name>` line before the rows.
- Cells are joined with tabs; rows with fewer cells produce fewer columns.
- Only the first 500 rows per sheet appear in the extracted text; the remainder
  is represented by `... N more rows omitted`. For the full table use
  `data.inspect` / `data.query`, and for one sheet pass `sheet` explicitly.

## DOCX Notes

- Only paragraph text, tabs and line breaks are captured; formatting, headers,
  footers, comments and images are not.
- Table cells are emitted as paragraph lines, so a table becomes a sequence of
  lines rather than a tab-separated grid. Do not claim cell coordinates.
- Stray whitespace and empty paragraphs are possible; normalise spacing in
  prose but keep the original wording for quotations.

## Raw Fallback Notes

- Unknown and binary extensions are read as text up to 20 MB and returned with
  the warning `file type "<ext>" has no dedicated processor; raw content
  returned as text`.
- Binary content can contain NUL bytes and unreadable runs; treat any extracted
  value as unverified until the format is confirmed.
- `files.describe` on a binary type reports only `extension` and `size_bytes`
  plus the same warning, so it cannot confirm structure.

## Error Catalogue

| Message | Cause | Fix |
|---|---|---|
| `input file not found: X` | Wrong path | Paths are relative to `data/input` |
| `legacy .xls (BIFF) ... not supported` | Old Excel format | Convert to `.xlsx` or `.csv` |
| `docx: word/document.xml not found` | Not a real DOCX or corrupt zip | Report the file as unreadable |
| `xlsx: no worksheets found` | Corrupt workbook | Report the file as unreadable |
| `artifact_id is required` | `documents.get` without an id | Use the `artifact_id` from `documents.ingest` |
| `no matches for the query` | `knowledge.search` found nothing | Rephrase once or drop `index_id`, then report absence |
