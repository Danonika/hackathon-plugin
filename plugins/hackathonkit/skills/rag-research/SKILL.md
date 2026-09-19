---
name: rag-research
description: "Use for questions over a document corpus that must be answered with citations: ingest, index, search, extract. Covers documents.ingest, knowledge.index, knowledge.search and documents.get."
---

# RAG Research

## When To Use

- The user asks a question that must be answered from documents (contracts, regulations, policies, reports).
- Several sources must be compared or verified against each other.
- A grounded answer with citations is required, not a summary from memory.

## Workflow

1. Inventory: `files.describe` for each candidate file in `data/input`.
2. Ingest: `documents.ingest` per file -> keep the returned `artifact_id`.
3. Index: `knowledge.index` per artifact (or per path). Default chunks: 800 chars, 100 overlap.
4. Search: `knowledge.search` with focused queries. Split broad questions into 2-4 concrete queries.
5. Read: if a snippet is insufficient, `documents.get` the artifact and read around the match.
6. Answer: only from retrieved text; cite `artifact_id` and chunk or page.

## Query Formulation

- Use the question's key nouns, not full sentences.
- Try synonyms when the first query returns nothing (the corpus may use different wording).
- One concept per query; merge results afterwards.

## Output Contract

- Direct quotes for critical claims, paraphrases elsewhere; both with the source.
- State coverage: "searched N chunks from M documents".
- Contradictions between sources: show both positions and which document is authoritative.
- If nothing relevant was found, say so and list the queries tried. Never fill gaps with general knowledge.

## Failure Modes

| Symptom | Action |
|---|---|
| `knowledge index is empty` | Run `knowledge.index` first; the tool says this explicitly |
| No hits | Rephrase with corpus vocabulary; verify the document was indexed |
| Hits are boilerplate (headers, footers) | Increase `chunk_chars` or index a cleaner extraction |
| Scanned PDF produced no text | Report the OCR limitation; do not index empty artifacts |

## Anti-Patterns

- Answering before searching.
- Citing a document that only vaguely matches while a direct source exists.
- Mixing retrieved facts with model knowledge without marking the boundary.
