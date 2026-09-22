# RAG Cookbook

Exact call sequence for grounded answers over `data/input` documents.

## End-to-End Sequence

```text
files.describe {path: "policy.docx"}          -> rows/columns/format
documents.ingest {path: "policy.docx"}        -> artifact_id: "policy-....txt", chars
knowledge.index {artifact_id: "...", corpus: "policy-qa",
                 chunk_chars: 800, overlap: 100} -> index_id, chunk_count
knowledge.search {query: "termination notice period", limit: 5,
                  corpus: "policy-qa", text_chars: 300} -> hits with chunk_id, score
documents.get {artifact_id: "...", offset: 1200, max_chars: 1500} -> context slice
```

Keep the mapping `file -> artifact_id -> index_id` for the whole session.

## Chunking Choices

| Document type | `chunk_chars` | `overlap` | Why |
|---|---|---|---|
| Contracts, laws | 800-1000 | 100-150 | clauses are self-contained but reference neighbours |
| Policies, manuals | 600-800 | 100 | sections are short |
| Reports, articles | 800-1200 | 100-200 | arguments span paragraphs |
| Tables exported to text | 400-600 | 50 | row groups stay together |
| Transcripts | 800 | 150 | speaker turns overlap |

Changing `chunk_chars` means re-indexing with a new `corpus` label or `index_id`.
Never silently mix chunkings under the same corpus.

## Query Patterns

- Use key nouns from the question, not full sentences.
- One concept per query; merge the hit lists afterwards.
- Try the corpus's own vocabulary: if the document says "termination", querying "dismissal" may miss.
- Scope with `corpus` or `index_id`/`index_ids` when several corpora exist.
- Start at `limit` 5, `text_chars` 300. Raise `text_chars` to 600-800 for legal or numeric clauses.
- Zero hits: re-read a known chunk with `documents.get`, extract its vocabulary, re-query.

| Question shape | Query 1 | Query 2 | Query 3 |
|---|---|---|---|
| "What is the notice period?" | `notice period` | `termination notice` | `cancellation days` |
| "Are there penalties?" | `penalty` | `late payment fee` | `liability cap` |
| "Who approves spend?" | `approval authority` | `spend limit` | `sign-off threshold` |

## Citation Format

| Claim type | Format |
|---|---|
| Direct quote | `"..." (artifact_id, chunk 12)` |
| Paraphrase | `paraphrase (index_id, chunk 7)` |
| Page available | `"..." (artifact_id, p. 4, chunk 12)` |
| Multiple sources | `(artifact_id A, chunk 3; artifact_id B, chunk 9)` |
| Contradiction | `A says X (chunk 3); B says Y (chunk 9); A is the signed original` |

## Worked Example

Question: "What is the termination notice period in the vendor contract?"

1. `files.describe {path: "vendor-contract.pdf"}` -> pdf, 42 pages, text layer present.
2. `documents.ingest {path: "vendor-contract.pdf"}` -> `artifact_id: "vendor-contract-a1b2.txt"`.
3. `knowledge.index {artifact_id: "vendor-contract-a1b2.txt", corpus: "vendor-qa"}` -> `index_id: "idx-77"`, 58 chunks.
4. `knowledge.search {query: "termination notice", limit: 5, corpus: "vendor-qa"}` -> top hit chunk 21, score 0.81.
5. `documents.get {artifact_id: "vendor-contract-a1b2.txt", offset: 8400, max_chars: 1200}` -> reads clause 14.2.
6. Answer: `"Either party may terminate with 30 days written notice" (vendor-contract-a1b2.txt, chunk 21). Coverage: 5 chunks searched from 1 document (58 indexed).`
7. Optional `reports.create` with the same evidence section.

## Reuse and Refresh

- Reuse `index_id`/`corpus` for follow-up questions in the same session.
- Re-index when: the file changed, chunking hid the answer, or a different sheet/worksheet was needed.
- Do not index the same artifact twice under the same corpus; search the existing index instead.

## Common Mistakes

- Indexing a `documents.extract` preview instead of the full ingested artifact.
- Searching before indexing and reporting "no answer" instead of "empty index".
- Quoting a summary paragraph while the operative clause is in another chunk.
- Forgetting `corpus` on a second question and searching another task's corpus.
