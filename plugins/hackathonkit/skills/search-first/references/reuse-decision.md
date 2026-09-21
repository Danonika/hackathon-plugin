# Reuse Decision Rubric

Criteria, checks and scoring for the adopt / extend / compose / build decision. Read this when the sweep found candidates and the choice is not obvious.

## Decision Criteria

| Decision | Definition | Choose when | Requirements before acting |
|---|---|---|---|
| adopt | Use an existing candidate unchanged | covers >= 80% of the need; API stable; license compatible | pin the version; record the integration point |
| extend | Use existing code with parameters, hooks or a patch | fit is close but one behavior is missing | keep the change local; add a test for the new behavior |
| compose | Wire two or more small existing pieces | no single fit; pieces are stable and small | document the wiring; avoid a new abstraction layer |
| build | Write new code | no candidate fits, or reuse costs more than building | state the rejected alternatives and their reasons |

Escalate toward `build` only after the earlier options were checked and recorded. Escalate toward `adopt` only when the integration cost is lower than the maintenance cost of new code.

## Pre-Decision Checklist

1. Does the repository already implement it? (`rg`, package listing)
2. Does an MCP tool compute it? (`system.capabilities`)
3. Does a prior artifact or decision cover it? (`data/artifacts`, `evals`, `knowledge.search`)
4. Is there a maintained dependency with a compatible license?
5. What is the total dependency weight of the candidate?
6. What is the integration cost (adapter, version constraint, runtime support)?
7. What is the exit cost if the candidate must be replaced later?

A candidate that fails question 4, or has no answer to question 7, does not get adopted.

## License Compatibility

| Candidate license | Adopt or copy into a shipping project? | Notes |
|---|---|---|
| MIT, BSD-2/3, Apache-2.0, ISC | yes | keep the notice; Apache-2.0 adds a patent grant |
| MPL-2.0, EPL-2.0 | yes with file-level care | modified files stay under the same license |
| LGPL-2.1/3.0 | only as an unmodified dynamically linked library | static linking changes the obligations; verify |
| GPL-2.0/3.0, AGPL-3.0 | no for closed or hosted distribution | network use triggers AGPL; treat as incompatible by default |
| No license / "all rights reserved" | no | absence of a license is not permission |
| Unclear or mixed | no until verified | record exactly what was unclear |

Copying a snippet is licensing the code: the same table applies to snippets, forum answers and generated code of unknown provenance.

## Maintenance Signals

| Signal | Healthy | Caution | Replace or fork |
|---|---|---|---|
| Last release | <= 12 months | 12-24 months | > 24 months |
| Open issues vs closed | declining or stable | growing slowly | growing with no maintainer response |
| Commit cadence | regular | occasional | stalled |
| Bus factor | 2+ maintainers | 1 maintainer | maintainer unresponsive |
| Security advisories | none open | one open with a patch | open without a patch |
| Ecosystem use | depended on by known projects | niche | abandoned forks only |

Stars and download counts are popularity, not maintenance. Use dates and issue state.

## Dependency Weight

- Read the manifest or lockfile and count the direct and transitive modules the candidate adds.
- Prefer zero-dependency or standard-library-only candidates for small needs.
- A single call site rarely justifies a heavy tree; compose or build instead.
- Heavyweight candidates are acceptable when they replace many lines or own a hard problem (crypto, parsing, protocols).
- Record the added count in the candidate table so the next session can audit it.

## Candidate Scoring

| Criterion | 0 | 1 | 2 | 3 |
|---|---|---|---|---|
| Fit | not usable | needs a fork | needs an adapter | usable as-is |
| Maintenance | abandoned | slow | regular | active, multi-maintainer |
| License safety | incompatible | unclear | compatible with obligations | permissive, no obligations |
| Dependency weight | heavy tree | several modules | one module | stdlib / none |
| Integration cost | weeks | days | hours | minutes |

Rules: any candidate with license safety 0 is rejected; ties are broken by dependency weight, then by fit; the scores are a guide, not arithmetic - the recorded rationale decides.

## Worked Example

Task: add retry logic for an HTTP client.

| Candidate | Type | Fit | License | Last activity | Deps | Score |
|---|---|---|---|---|---|---|
| `internal/tools/http.go` helper | local | 2 (needs a parameter) | project license | current | 0 | 8 |
| `github.com/cenkalti/backoff/v4` | registry | 3 | MIT | active | 6 modules | 9 |
| hand-written 20-line loop | build | 3 | n/a | n/a | 0 | 6 |

Outcome: extend the local helper - no new code path, one parameter, and the registry option loses on dependency weight for a single call site. The decision artifact records candidates, scores and rejection reasons.

## Recording Template

`reports.create` with `title: "Reuse decision - <task>"` and sections: Need, Candidates, Decision, Rationale, License and maintenance, Risks, Next step. One row per candidate; no candidate row without a license and last-activity value.

## Anti-Patterns

- Scoring a candidate without reading its license file.
- Treating a missing license as "free to use".
- Adopting a library for one function call.
- Forking before checking whether upstream already has the feature.
- Recording only the winner and losing the rejected alternatives.
