---
name: document-intelligence
description: "Use for document tasks: text extraction, structure discovery, field extraction, classification, summaries and comparisons across txt, md, docx, xlsx, pdf and other text formats."
---

# Document Intelligence

## When To Use

- The user provides documents and expects extracted values, summaries, comparisons or classification.
- Mixed document types arrive in one task and must be routed by kind.
- A PDF, DOCX or XLSX must be converted into analysable text.

## Workflow

1. `files.describe` - determine type, size and structure (XLSX: `sheets`).
2. Extract:
   - One-off reading: `documents.extract`.
   - Reusable or large: `documents.ingest` -> `artifact_id`, then index or read via `documents.get`.
3. Mixed batch: `classification.classify` to group documents before per-type processing.
4. Field extraction: pull only values the text supports; keep the surrounding sentence as evidence.
5. Deliver: `reports.create` with one section per document or per extracted field group.

## Format Notes

| Format | Behaviour |
|---|---|
| txt, md, csv, tsv, json, log | Raw text |
| docx | Paragraph text from `word/document.xml`; tables become tab-separated lines |
| xlsx | Every worksheet, prefixed with `## Sheet: <name>` |
| pdf | Text layer only; scanned PDFs return an explicit warning and empty text |
| Other | Raw content plus a warning that no dedicated processor exists |

## Output Contract

- Extracted fields with source location ("page 4", "row 17", "chunk 3").
- `truncated: true` must be surfaced; large documents need `ingest` + `knowledge.index` instead of inline text.
- For comparisons: align on field names in a table, then list differences, then list gaps (fields present in only one document).

## Failure Modes

| Symptom | Action |
|---|---|
| `pdf contains no extractable text` | Report the OCR limitation; offer alternative sources |
| `truncated: true` | Switch to `ingest` + `knowledge.search` |
| XLSX sheet missing | Re-check `sheets` from `files.describe` |
| DOCX text has stray whitespace | Normalize spacing before quoting; keep original for citations |

## Anti-Patterns

- Guessing field values that are not in the extracted text.
- Treating a summary of a truncated document as complete.
- Classifying by filename without reading content when the task is ambiguous.
