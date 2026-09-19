---
name: report-builder
description: Use when the user needs a final artifact: report, summary, findings document or presentation text. Covers reports.create and artifact evidence.
---

# Report builder

Structure every report as:

1. Task and input (files, artifact ids).
2. Method (tools used, parameters, thresholds).
3. Findings with numbers and sources.
4. Limitations and warnings.
5. Next steps.

Use `reports.create` with `sections: [{heading, body}]`; `format` is `md` (default) or `html`.

Rules:

- Only include numbers that tools returned.
- Keep source references from tool responses (artifact ids, timestamps, rows).
- Explicitly list warnings from tool responses in the limitations section.
