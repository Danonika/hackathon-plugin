---
name: project-cost
description: Internal technical readiness score 0-100 for the current snapshot. Evidence-only. Explicit-only: never invoke implicitly.
---

# Project cost

Technical readiness score, not a price. Evidence-only.

| Criterion | Max |
|---|---:|
| Case fit and functionality | 20 |
| Technical implementation | 25 |
| README and technical documentation | 20 |
| Reproducibility and deployment readiness | 20 |
| Reliability and security basics | 15 |
| **Total** | **100** |

## Procedure

1. Score only what is confirmed by code, tests, smoke artifacts, README sections, build results or demo artifacts.
2. Reproducibility cannot receive full score unless the clean-room gate passed.
3. Output JSON: `technical_score`, per-criterion `scores`, `blocking_issues`, `deductions`, `must_fix_next`.
4. Compare with the previous snapshot; a regression means rollback, not negotiation.

Never award points for claimed functionality without an artifact.
