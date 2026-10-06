# Fixing the OCR "slope" (findings that never converge)

## The problem

`ocr scan` (full-file scan) is **not reproducible**. Measured on this repo:

- Three full scans of *identical* code: 432 / 444 / 424 comments, 56-72% location overlap.
- Two full scans ~3h apart (during which ~96 findings were fixed): only **124 of 640** unique
  locations shared — each scan reports a different ~20% of the pool.
- Same file, scanned twice: 6 vs 5 findings, **2/6** overlap (default settings).
- Same file, twice, with `--no-plan --no-dedup --no-summary --concurrency 1`: **4/5** overlap.
- Setting `temperature=0` alone did **not** help (the variance is agentic, not sampling).

So per-scan counts are a random sample, not a progress metric. Fixing code also *adds* code, which
adds findings. That is the "slope".

## The fix (process, not more scanning)

### 1. Scan deterministically
Disable the variance-producing subtasks and parallelism:

```sh
ocr scan --no-plan --no-dedup --no-summary --concurrency 1 --format json -o scan.json --audience agent
```

This roughly doubles repeat-scan overlap. It is still not 100%, which is why step 2 matters.

### 2. Keep one canonical, deduplicated backlog
`scripts/ocr-dedup.py` gives every finding a **stable signature** = `path + flagged code`
(line numbers excluded, so edits that shift lines do not create a new identity). It merges every
scan into `docs/ocr-baseline.json` and reports **open unique findings** — a metric that only moves
when a finding is actually triaged.

```sh
# deterministic scan of the whole repo, merged into the canonical backlog
scripts/ocr-baseline.sh

# or a single path
scripts/ocr-baseline.sh --path internal/tools

# readable summary
docs/OCR-CANONICAL.md
```

### 3. Triage once, and it stays triaged
Every finding has a status. Suppressed statuses (`false-positive`, `wontfix`, `accepted`) are
preserved across re-scans, so accepted patterns stop inflating the count. `fixed` is set when the
code changed.

```sh
scripts/ocr-baseline.sh --set false-positive --match <sig>
scripts/ocr-baseline.sh --set fixed         --match <sig>
```

### 4. Gate on diffs, not whole files
For regressions (CI / pre-merge), review only what changed. This bounds the finding set to the
change instead of the whole repo:

```sh
ocr review --from <base> --to HEAD --format json -o review.json --audience agent
```

### 5. Gate only on critical/high
Treat medium/low as an advisory backlog. `docs/ocr-baseline.json` records severity, so a gate can
fail only when an **open critical/high** exists:

```sh
python3 scripts/ocr-dedup.py --baseline docs/ocr-baseline.json --summary scan.json \
  | awk -F, '$1=="critical"||$1=="high"{if($2>0) exit 1}'
```

## Recommended workflow

1. `scripts/ocr-baseline.sh` after a batch of changes → updates the canonical backlog.
2. Work findings by signature from `docs/OCR-CANONICAL.md`, one at a time, verifying each with
   `ocr scan --path <file> --no-plan --no-dedup --no-summary --concurrency 1`.
3. When a fix lands, mark it `fixed`; when a finding is rejected, mark it `false-positive`/`wontfix`.
4. Watch **open unique findings** by severity — that is the only number that should trend down.
5. In CI, use `ocr review` on the PR diff and gate on open critical/high only.

## What NOT to do

- Do not run repeated full scans and compare raw counts.
- Do not fix findings in large parallel batches (this session's `v0.16.86` sweep introduced a
  critical and ~13 new highs — see `docs/OCR-SESSION-LOG.md`).
- Do not treat "findings reported per scan" as progress.
