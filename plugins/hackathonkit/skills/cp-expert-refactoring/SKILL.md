---
name: cp-expert-refactoring
description: Algorithmic and performance pass over hot paths like a competitive programmer, without code golf. Explicit-only.
---

# CP expert refactoring

Review: asymptotic complexity, data structures, hot loops, allocations, duplicate passes, parsing cost, boundary conditions, integer/float handling, determinism.

Rules:

1. Preserve public behavior.
2. Preserve tests.
3. Prefer simpler complexity.
4. Do not sacrifice readability for three-line tricks.
5. Every non-trivial optimization states: before complexity, after complexity, expected benefit, tests protecting behavior.

Apply fixes through the normal coding flow and re-run tests afterwards.
