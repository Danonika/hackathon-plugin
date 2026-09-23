# Coverage Policy

- Default target: 80% repo-wide, enforced by `scripts/coverage-check.sh`.
- Override per run: `HACKATHONKIT_COVERAGE_TARGET=N` or `/verify --target N`.
- The check fails the gate when the total is below the target.
- Coverage is a floor, not a goal: prefer meaningful tests over hitting the
  number. Never lower the target to pass.
- A coverage failure names the total and the target; add tests, then rerun the
  whole gate (not just the coverage check), because the sha may have changed.
