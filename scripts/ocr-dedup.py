#!/usr/bin/env python3
"""Merge OpenCodeReview scan results into one canonical, deduplicated backlog.

The problem this solves: `ocr scan` is non-reproducible (each run reports a
different ~20-50% of a large pool), so per-scan counts never go down and the
same bug is "found" forever. This tool gives every finding a stable signature,
merges every scan into a single union list with a triage status, and reports a
monotonic "open unique findings" metric.

Usage:
  ocr-dedup.py --baseline docs/ocr-baseline.json scan1.json [scan2.json ...]

Options:
  --baseline FILE   canonical backlog JSON to update (created if absent)
  --out-json FILE   write the merged backlog here (default: --baseline)
  --out-md FILE     write a human-readable summary here
  --set STATUS      apply STATUS to every finding in the scans
                    (open|fixed|false-positive|wontfix|accepted)
  --match SIG       with --set, only change the finding whose sig starts with SIG
  --open            print only OPEN findings to stdout (sig, severity, path:line)
  --summary         print a severity summary to stdout

Status semantics:
  open           not yet fixed
  fixed          code changed so it no longer applies
  false-positive reviewed and rejected
  wontfix        accepted risk
  accepted       intentional pattern (suppress in future runs)

A finding seen again while status is fixed/false-positive/wontfix/accepted stays
suppressed; only `open` counts toward the metric.
"""

from contextlib import contextmanager
import argparse
from pathlib import Path
import hashlib
import json
import os
import re
import sys
import tempfile
import time


def norm(s):
    return re.sub(r"\s+", " ", (s or "").strip())


def signature(path, finding):
    """Stable identity: path + the flagged code, falling back to the message.

    Line numbers are deliberately excluded so edits that shift lines do not
    create a new identity.
    """
    anchor = norm(finding.get("existing_code")) or norm((finding.get("content") or "")[:240])
    h = hashlib.sha1(f"{path}|{anchor}".encode("utf-8", "replace")).hexdigest()[:12]
    return h


def load_json(path):
    with open(path, "r", encoding="utf-8") as fh:
        return json.load(fh)


def merge(baseline, scans, now):
    findings = baseline.setdefault("findings", {})
    new_sigs = []
    scanned_sigs = set()
    for scan in scans:
        comments = scan.get("comments")
        if not isinstance(comments, list):
            raise ValueError("OCR result must contain a comments list")
        for c in comments:
            path = c.get("path") or ""
            if path.startswith("cmd/demo-api/static/"):
                continue  # replaced frontend; not actionable
            sig = signature(path, c)
            scanned_sigs.add(sig)
            title = norm(c.get("content"))[:160]
            entry = findings.get(sig)
            if entry is None:
                entry = {
                    "sig": sig,
                    "path": path,
                    "line": c.get("start_line"),
                    "severity": c.get("severity"),
                    "category": c.get("category"),
                    "title": title,
                    "status": "open",
                    "first_seen": now,
                    "last_seen": now,
                    "seen_count": 0,
                }
                findings[sig] = entry
                new_sigs.append(sig)
            else:
                # A finding seen again reopens only if it was open; suppressed
                # statuses (fixed/false-positive/wontfix/accepted) stay put.
                entry["line"] = c.get("start_line")
                entry["severity"] = c.get("severity")
                entry["title"] = title
                entry["last_seen"] = now
            entry["seen_count"] = entry.get("seen_count", 0) + 1
    return findings, new_sigs, scanned_sigs


def severity_counts(findings, statuses=("open",)):
    counts = {}
    for e in findings.values():
        if e.get("status") in statuses:
            counts[e.get("severity", "?")] = counts.get(e.get("severity", "?"), 0) + 1
    return counts


@contextmanager
def atomic_output(path):
    directory = Path(path).resolve().parent
    directory.mkdir(parents=True, exist_ok=True)
    tmp = tempfile.NamedTemporaryFile(mode="w", dir=directory, delete=False, encoding="utf-8")
    try:
        with tmp as fh:
            yield fh
        if os.path.exists(path):
            os.chmod(tmp.name, os.stat(path).st_mode & 0o777)
        os.replace(tmp.name, path)
    finally:
        Path(tmp.name).unlink(missing_ok=True)


@contextmanager
def update_lock(destination):
    # Keep the lock inode in place across atomic replacement of the backlog.
    lock = Path(str(destination) + ".lock")
    lock.parent.mkdir(parents=True, exist_ok=True)
    with lock.open("a+b") as fh:
        if os.name == "nt":
            import msvcrt
            fh.seek(0)
            msvcrt.locking(fh.fileno(), msvcrt.LK_LOCK, 1)
        else:
            import fcntl
            fcntl.flock(fh, fcntl.LOCK_EX)
        yield


def main():
    ap = argparse.ArgumentParser(description="Merge OCR scans into a canonical backlog.")
    ap.add_argument("scans", nargs="*")
    ap.add_argument("--baseline", required=True)
    ap.add_argument("--out-json")
    ap.add_argument("--out-md")
    ap.add_argument("--set", dest="set_status", choices=["open", "fixed", "false-positive", "wontfix", "accepted"])
    ap.add_argument("--match", help="with --set, only the finding whose sig starts with this")
    ap.add_argument("--open", action="store_true", help="print open findings")
    ap.add_argument("--summary", action="store_true")
    args = ap.parse_args()

    target = args.out_json or args.baseline
    if args.out_md and Path(args.out_md).resolve() == Path(target).resolve():
        ap.error("Markdown and JSON outputs must have different paths")
    if args.set_status or args.scans or args.out_json or args.out_md:
        with update_lock(target):
            process(args, ap)
    else:
        process(args, ap)


def process(args, ap):
    now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    if os.path.exists(args.baseline):
        baseline = load_json(args.baseline)
    else:
        baseline = {"version": 1, "findings": {}}

    scans = [load_json(p) for p in args.scans]
    findings, new_sigs, scanned_sigs = merge(baseline, scans, now)

    if args.match is not None and not args.set_status:
        ap.error("--match requires --set")
    if args.set_status:
        if args.match is not None:
            matches = [sig for sig in findings if args.match and sig.startswith(args.match)]
            if len(matches) != 1:
                ap.error("--match must uniquely identify one finding")
        elif scans:
            matches = scanned_sigs
        else:
            ap.error("--set requires --match or an input scan")
        for sig in matches:
            findings[sig]["status"] = args.set_status
        print(f"[ocr-dedup] set status={args.set_status} on {len(matches)} finding(s)", file=sys.stderr)

    if scans or args.set_status or args.out_json:
        with atomic_output(args.out_json or args.baseline) as fh:
            json.dump(baseline, fh, indent=2, sort_keys=True)
            fh.write("\n")

    open_counts = severity_counts(findings, ("open",))
    total_open = sum(open_counts.values())
    severity_summary = ", ".join(f"{key}={value}" for key, value in sorted(open_counts.items()))
    breakdown = f" ({severity_summary})" if severity_summary else ""
    print(
        f"[ocr-dedup] merged {len(scans)} scan(s): "
        f"{len(new_sigs)} new, {total_open} open unique{breakdown}",
        file=sys.stderr,
    )

    if args.summary:
        print("severity,open")
        for k in ("critical", "high", "medium", "low", "?"):
            if open_counts.get(k):
                print(f"{k},{open_counts[k]}")

    if args.open:
        for sig, e in sorted(findings.items(), key=lambda kv: (kv[1].get("severity") or "z", kv[1].get("path") or "")):
            if e.get("status") == "open":
                print(f"{sig}\t{e.get('severity')}\t{e.get('path')}:{e.get('line')}\t{e.get('title','')[:80]}")

    if args.out_md:
        target_path = Path(args.out_json or args.baseline).resolve()
        with atomic_output(args.out_md) as fh:
            fh.write("# Canonical OCR backlog\n\n")
            fh.write(f"Updated: {now}\n\n")
            fh.write(f"Open unique findings: **{total_open}**{breakdown}\n\n")
            counts = {}
            for entry in findings.values():
                status = entry["status"]
                counts[status] = counts.get(status, 0) + 1
            fh.write("| Disposition | Findings |\n| --- | ---: |\n")
            for status, count in sorted(counts.items()):
                fh.write(f"| {status} | {count} |\n")
            try:
                json_link = os.path.relpath(target_path, Path(args.out_md).resolve().parent)
            except ValueError:  # Windows Markdown and JSON outputs on different drives.
                fh.write(f"\nJSON backlog: {target_path}\n\n")
            else:
                fh.write(f"\nIndividual findings and review notes are in the [JSON backlog](<{Path(json_link).as_posix()}>).\n\n")
            for sev in ("critical", "high", "medium", "low", "?"):
                rows = [e for e in findings.values() if e.get("status") == "open" and e.get("severity") == sev]
                if not rows:
                    continue
                fh.write(f"## {sev.upper()} ({len(rows)})\n\n")
                for e in sorted(rows, key=lambda z: (z.get("path") or "", z.get("line") or 0)):
                    fh.write(f"- [ ] `{e['sig']}` `{e['path']}:{e['line']}` _{e.get('category')}_ {e.get('title','')}\n")
                    if e.get("review_note"):
                        fh.write(f"  {e['review_note']}\n")
                fh.write("\n")


if __name__ == "__main__":
    main()
