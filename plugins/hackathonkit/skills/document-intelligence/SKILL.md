---
name: document-intelligence
description: Use for PDF/DOCX/TXT/XLSX document tasks: text extraction, field extraction, classification, summaries and document comparisons.
---

# Document intelligence

1. `files.describe` to identify type and structure.
2. `documents.extract` for text; `documents.ingest` when the text must be stored and indexed.
3. `classification.classify` for type routing when several document kinds are mixed.
4. For fields, extract only what the text supports and keep the source location.

Rules:

- PDF extraction is limited in v0.1: report the limitation instead of guessing content.
- Preserve document structure (headings, tables) when summarizing.
- Never silently drop tables: mention them as structured data and use `data.inspect` when converted.
