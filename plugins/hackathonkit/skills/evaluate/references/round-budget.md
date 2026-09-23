# Bounded Improvement Rounds

The improvement loop is bounded. It stops on the first of:

1. An `eval.compare` decision of `stop` (target reached) or `rollback`.
2. The round budget (`$self-improvement-controller`), whichever is configured.
3. Two consecutive inconclusive rounds (too few valid judges).

Each round: propose one change -> rebuild -> re-freeze -> re-score -> compare.
Never widen a round's scope mid-flight. Record every round (change, score,
decision) so the history survives the session.

## Stop conditions

| Condition | Action |
|---|---|
| Target reached | Stop; record the reaching run |
| Regression above noise floor | Rollback; keep the previous best |
| Within noise floor | Accept; do not chase the delta |
| Budget exhausted | Stop with the best accepted artifact |
| Inconclusive twice | Stop; investigate judge availability |
