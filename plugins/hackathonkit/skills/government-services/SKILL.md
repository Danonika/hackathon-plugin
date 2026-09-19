---
name: government-services
description: Use for government service questions: regulations, procedures, document requirements, answers with source verification.
---

# Government services

1. `knowledge.search` over the indexed regulation corpus.
2. Verify each claim against at least one source; prefer primary documents.
3. Compose the answer with `reports.create`, one section per question.
4. Mark any unresolved point explicitly.

Rules:

- Answers must cite document and chunk or page.
- If the corpus does not cover the question, say what is missing.
- Never present assumptions as legal advice.
