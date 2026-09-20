---
name: theme-research
description: "Use when a build needs external research on the hackathon theme: domain, users, prior art, APIs, regulations, competitors, judging criteria. Produces a cited brief with confidence and gaps. For questions over provided documents use rag-research instead."
---

# Theme Research

Research the theme before building. Findings feed `$build-planner` and feature decisions, not trivia.

## When To Use

- The task statement names a domain the team does not know well (fintech, healthcare, logistics, govtech, education).
- A feature, data-source or API decision depends on facts outside the repository.
- A likely judge question or risk needs an external source.
- Not for questions answerable from `data/input` documents - use `$rag-research` for those.

## Rules

- The MCP layer has no internet access. Use the host's web search/fetch when available; otherwise research only the provided corpus and mark every external gap explicitly.
- Never invent a source, URL, statistic or quote. No source, no claim.
- Prefer primary sources: official docs, laws, standards, statistics, vendor API references. Blog posts are leads, never the source of a number.
- Record the retrieval date for every source; separate observed fact from interpretation.

## Workflow

1. **Scope**: turn the theme into 3-6 research questions. For each, write the decision it informs and the evidence that would settle it.
2. **Isolated passes**: one fresh pass per question - parallel sub-agents when the host supports them, otherwise sequential passes with no carry-over. Each pass returns only: answer, evidence table, confidence, open gaps.
3. **Gather**: local corpus first when it exists (`$rag-research`), then host web search/fetch. Keep raw findings with source ids.
4. **Evidence table**: claim | source (URL or `artifact_id`/chunk) | date | confidence (high/medium/low) | independent confirmations.
5. **Synthesize**: per question - answer, implications for the build (features, constraints, risks), open questions.
6. **Persist**: `reports.create` a research brief (`theme-research-<slug>`), hand it to `$build-planner` and keep the `artifact_id` for the judge evidence bundle.

## Confidence

- **high** - primary source, or 2+ independent sources agree.
- **medium** - single credible source, or a primary source only partially applicable.
- **low** - secondary source, opinion or extrapolation; label it and never base a critical decision on it alone.

## Output Contract

- Sections: Scope, Questions and answers, Evidence table, Implications for the build, Risks and unknowns, Sources.
- Every claim carries a source and a date; unsupported claims are marked `unverified`.
- Ends with "What would change our mind": the evidence that would invalidate the conclusions.

## Quality Checks

- Could a judge open every source and see the claim?
- Is every number traced to a primary source?
- Are gaps stated as gaps instead of silently filled?

## Anti-Patterns

- Research without a decision it informs.
- Treating model memory as a source.
- Hiding missing web access; silently substituting general knowledge.
- Mixing retrieved facts with speculation in the same sentence.

## References

- `references/research-protocol.md` - question templates, source hierarchy, confidence rubric, per-pass output format
