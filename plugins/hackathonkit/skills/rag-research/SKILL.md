---
name: rag-research
description: Use when the user asks a question over a document corpus, needs grounded answers with citations, or asks to index documents for search. Covers documents.ingest, knowledge.index and knowledge.search.
---

# RAG research

1. `documents.ingest` each source file (returns `artifact_id`).
2. `knowledge.index` per artifact (or per input file path).
3. `knowledge.search` with focused queries; read returned snippets.
4. If snippets are insufficient, use `documents.get` for full context.
5. Answer only from retrieved text and cite `artifact_id` plus chunk or page.

Rules:

- If search returns no hits, say so plainly; do not fabricate content.
- Separate direct quotes from paraphrases.
- Track contradictions between sources and report them.
