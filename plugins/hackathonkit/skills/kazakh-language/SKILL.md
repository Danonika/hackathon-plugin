---
name: kazakh-language
description: "Use for Kazakh-language work: transcription of kk audio, text normalization, subtitle generation and language quality checks."
---

# Kazakh Language

## When To Use

- Audio or video contains Kazakh speech and must be transcribed.
- Kazakh text needs normalization (apostrophes, spacing, mixed alphabets).
- Subtitles or transcripts must be produced for a Kazakh audience.

## Workflow

1. `audio.transcribe` with `language: "kk"`. Without a language hint the model may drift to Russian.
2. Inspect the transcript: flag garbled fragments instead of guessing words.
3. Normalize:
   - Unify apostrophe variants (`'`, `’`, `` ` ``) to a single form.
   - Fix spacing around punctuation; keep sentence case as spoken.
   - Preserve proper names, numbers, dates and abbreviations exactly.
4. For subtitles: split by sentence with max ~42 characters per line and 2 lines per cue; keep cue timings monotonic.
5. Deliver through `reports.create` or as a subtitle artifact; keep the raw transcript artifact untouched.

## Quality Rules

- Do not translate silently; if a translation is requested, produce it as a separate section and keep the original.
- Mark low-confidence fragments with a note rather than replacing them with plausible words.
- Mixed Kazakh/Russian speech is normal; do not "fix" code-switching.
- When the audio quality fails (music, overlapping speakers), report it; partial transcripts are acceptable if labeled.

## Output Contract

- Transcript artifact id, character count, language hint used.
- Normalization decisions listed briefly (what was changed and why).
- For subtitles: number of cues and total duration covered.

## Failure Modes

| Symptom | Action |
|---|---|
| `OPENAI_API_KEY is not configured` | Report the requirement; offer text-based alternatives |
| Transcript is mostly Russian | Retry with `language: "kk"` explicitly; note if it stays mixed |
| No timestamps in the tool response | State that cues are sentence-aligned, not time-aligned |
