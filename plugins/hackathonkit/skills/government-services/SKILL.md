---
name: government-services
description: "Use for government service questions: procedures, document requirements, deadlines and answers grounded in an indexed regulation corpus with source verification."
---

# Government Services

## When To Use

- The user asks how to obtain a service, which documents are required, or what a regulation says.
- A citizen-facing answer must cite the actual source document.
- Several regulations may interact and one authoritative source must be chosen.

## Workflow

1. Index the corpus: `documents.ingest` + `knowledge.index` per regulation.
2. Search: `knowledge.search` per question, one concrete query per service or requirement.
3. Verify: prefer primary sources (laws, rules, official lists) over summaries; when two sources conflict, cite both and state which prevails and why.
4. Answer: `reports.create` with one section per question: answer, requirements list, procedure steps, source.
5. Mark unresolved points explicitly; do not extrapolate.

## Answer Contract

- Every legal claim carries an artifact citation (document + chunk).
- Requirements and steps as numbered lists, not prose paragraphs.
- Deadlines, fees and responsible bodies exactly as in the source; if absent, say "not found in the provided corpus".
- Coverage statement: how many documents were searched.

## Rules

- This is document research, not legal advice; say so once, briefly, without disclaimers on every paragraph.
- Never merge requirements from different services unless the source does.
- When the corpus misses a topic entirely, list what is missing instead of answering from general knowledge.

## Failure Modes

| Symptom | Action |
|---|---|
| No hits | Try official terminology variants; report the queries attempted |
| Source is outdated vs a newer document in the corpus | Prefer the newer document; note the date |
| Question spans several regulations | One section per regulation, then a combined summary |
