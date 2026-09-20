# Research Protocol

## Question Templates

Every research question must name the decision it informs.

| Theme area | Question shape |
|---|---|
| Problem and users | Who suffers from this problem today, how often, and what do they do instead? |
| Existing solutions | Which products/services solve it, and what do they not cover? |
| Data and APIs | Which official API or dataset provides this, with what limits, cost and license? |
| Regulation | Which law/standard applies, and what does it require of this workflow? |
| Judging | What do hackathon judges in this track reward, based on the published criteria? |
| Feasibility | What is the smallest evidence that this can work within the build time? |

## Source Hierarchy

1. Official documentation, laws, standards, government statistics.
2. Vendor API references, changelogs, pricing pages.
3. Peer-reviewed or institutional reports.
4. Reputable industry analysis (for context only).
5. Blogs, forums, social posts (leads only; verify elsewhere).

If a claim cannot reach level 1-3, it stays `low` confidence.

## Per-Pass Output Format

Each isolated pass returns exactly this, no prose padding:

```json
{
  "question": "Which official API provides X?",
  "answer": "One paragraph, no hedging beyond the confidence level.",
  "evidence": [
    {"claim": "Rate limit is 60 rpm", "source": "https://...", "date": "2026-09-21", "confidence": "high"}
  ],
  "implications": ["Add a cache layer before calling it per row"],
  "gaps": ["Free tier pricing not published"],
  "what_would_change_our_mind": ["A pricing page showing per-call billing"]
}
```

## Fallback Matrix

| Situation | Action |
|---|---|
| Host has no web access | Research the provided corpus only; list external gaps explicitly; do not guess |
| Sources contradict | Report both, name which is authoritative and why |
| Only low-confidence sources | State the conclusion is provisional and add a verification task to the plan |
| Question not answerable in time | Mark it open; `$build-planner` decides whether it blocks the build |

## Anti-Patterns

- Padding the brief with restated questions.
- One source cited for many different claims.
- Ignoring the retrieval date on time-sensitive facts (pricing, limits, laws).
