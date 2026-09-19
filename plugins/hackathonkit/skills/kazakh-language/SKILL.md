---
name: kazakh-language
description: Use for Kazakh-language tasks: transcription of kk audio, normalization of Kazakh text, subtitle generation and language checks.
---

# Kazakh language

1. `audio.transcribe` with `language: "kk"` for Kazakh audio.
2. Normalize: unify apostrophe variants, fix common OCR/ASR artifacts, keep proper names as spoken.
3. Validate timestamps and paragraph boundaries before building subtitles.
4. Deliver via `reports.create` or a subtitle artifact in `data/artifacts`.

Rules:

- Do not translate silently; keep the original transcript artifact.
- Flag low-confidence or garbled fragments instead of inventing words.
- Keep numbers, dates and names exactly as transcribed.
