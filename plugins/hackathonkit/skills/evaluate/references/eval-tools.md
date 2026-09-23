# eval.* Tool Reference

## freeze_bundle

`eval.freeze_bundle { profile: demo|technical|full, out_dir? }`
Copies a read-only snapshot, writes `git-sha.txt`, `rubric-version.txt`,
`bundle.json` and `bundle-hash.txt`. The hash covers every copied file except
`bundle.json` and `bundle-hash.txt`. Re-freezing the same tree yields the same
hash.

## aggregate

`eval.aggregate { reports:[...] }`
Drops invalid reports, conflicted judges, reports for another snapshot sha and
malformed payloads. Requires at least `min_valid_judges` (default 3) valid
scores; fewer is inconclusive, never zero.

## calibrate

`eval.calibrate { run1, run2, min_delta, min_valid_judges }`
Compares two runs and returns the retest delta and the noise floor. A decision
within the noise floor is not a real change.

## rank

`eval.rank { aggregates:[...], tie_break_order?, commission? }`
Applies regulation 9.6. Ties are resolved per tier by the commission (>=3
members, one vote each, strict majority per tier).

## compare

`eval.compare { candidate, best, noise_floor, target?, mode? }`
The only source of accept/rollback/stop/reached decisions.
