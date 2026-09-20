---
name: kazakh-language
description: "Use when audio, video or text must be processed as Kazakh: kk transcription, transcript normalization, subtitle cues and language quality checks. Produces a raw transcript artifact plus a listed normalization log. Do not use for translation-only jobs or pure Russian transcription unless the audio is mixed kk/ru."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: domain
tools:
  - files.describe
  - audio.transcribe
  - documents.extract
  - documents.get
  - reports.create
---

# Kazakh Language

Transcribe, normalize and package Kazakh-language content without inventing words, dropping national letters or fabricating timings.

## When To Use

- Audio or video contains Kazakh speech and must be transcribed (`language: "kk"`).
- Kazakh text needs normalization: apostrophes, spacing, punctuation, mixed alphabets.
- Subtitles or a clean transcript are required for a Kazakh audience.
- A language-quality pass is needed before publishing Kazakh copy.
- Do not use for translation-only work or for speech that is entirely Russian; use `$document-intelligence` for general text extraction and `$report-builder` for a prose deliverable.

## Input Triage

| Input | Tool path | Watch for |
|---|---|---|
| Audio or video with Kazakh speech | `files.describe` -> `audio.transcribe(language: "kk")` | No timestamps and no per-segment confidence in the envelope |
| Plain text or subtitle file | `files.describe` -> `documents.extract` | Encoding damage (`?`, mojibake) and truncation |
| Stored transcript artifact | `documents.get(artifact_id, offset, max_chars)` | Slices cut mid-sentence; continue with windows |
| Document (docx/pdf/xlsx) | `documents.extract` | Scanned PDFs yield no text layer |
| Mixed kk/ru audio | `audio.transcribe(language: "kk")` | Keep switches; never translate them away |

## Language Cues

- Kazakh-specific letters: **ә ғ қ ң ө ұ ү һ і**. Their presence marks a Kazakh passage; their absence does not prove the text is Russian, because Kazakh also uses Russian letters.
- Common Kazakh function words (және, бірақ, үшін, қалай, болып, емес) support the classification; never rewrite dialect or regional variants toward them.
- Russian-only patterns: ё, ъ, and endings such as -ого, -его, -ться.
- Code-switching is normal in speech: classify sentence by sentence, not the whole recording.
- When signals conflict, report a mixed-language result instead of forcing one label.

## Core Principles

1. Language hint is mandatory: call `audio.transcribe` with `language: "kk"`; without a hint the model drifts toward Russian on mixed audio.
2. Never guess garbled words: mark an uncertain span instead of replacing it with a plausible word. A labeled gap beats a silent invention.
3. Normalize surface, not meaning: apostrophes, spaces, dashes and quotes change; names, numbers, dates and word choice do not.
4. Code-switching is data: mixed Kazakh/Russian speech is normal; keep it and mark it, never translate it away.
5. The raw transcript artifact is immutable; every normalization produces a new artifact and a change log.
6. No timestamps means no timestamps: `audio.transcribe` returns text only, so cues are sentence-aligned, never time-aligned.

## Workflow

1. Identify the input with `files.describe` (extension, size, `has_timestamp`). Audio/video goes to step 2; a text file goes to step 4 via `documents.extract`.
2. Transcribe with `audio.transcribe`: `path` relative to `data/input`, `language: "kk"` (use `"ru"` only when the audio is known to be Russian). Keep `data.artifact_id`, `data.chars` and the envelope warning list.
3. Inspect the transcript: split into sentences, flag garbled spans as `[түсініксіз N]` placeholders and count them; never invent timestamps.
4. For text input, read it with `documents.extract` (`max_chars` as needed) or `documents.get` (`artifact_id`, `offset`, `max_chars`); for long documents work in overlapping windows and keep the offsets.
5. Normalize with the rules in `references/normalization.md`: unify apostrophes to U+02BC, collapse whitespace variants, fix spacing around punctuation, unify dash and quote forms, and keep Kazakh letters ә ғ қ ң ө ұ ү һ і intact.
6. Flag, do not silently convert, Latin homoglyphs (a/e/o/p/c/x/y inside Cyrillic words) and unexpected Russian letter substitutions (и for і, у for ұ/ү); list each flag with its location.
7. Build subtitles when asked: sentence per cue, max ~42 characters per line, max 2 lines per cue, monotonic ordering, and the sentence-alignment caveat stated in the output.
8. Publish with `reports.create` (`format: "md"`): raw transcript reference, normalized text, normalization log, flagged spans, subtitle block, coverage.
9. Never overwrite the raw transcript artifact; the report links to it by `artifact_id`.

## Subtitle Rules

| Rule | Value | Why |
|---|---|---|
| Characters per line | ~42 maximum | Readability on small screens |
| Lines per cue | 2 maximum | Standard subtitle convention |
| Cue unit | One sentence, split at clause boundaries | Timings are unavailable |
| Ordering | Monotonic, source sentence order | Cues are sentence-aligned |
| Placeholder | `[түсініксіз N]` stays inside the cue | The flag must remain visible |
| Raw text | Never overwritten | Normalization stays auditable |

State the sentence-alignment caveat once in the report; never present cues as time-accurate.

## Decision Matrix

| Signal | Action |
|---|---|
| Transcript is mostly Russian | Retry once with `language: "kk"`; if it stays Russian, report that the audio is not Kazakh |
| Mixed kk/ru sentences | Keep both languages; mark the switch; do not translate |
| Garbled span | Insert a labeled placeholder; never guess the missing word |
| Kazakh letter replaced by a Russian lookalike (и/і, у/ұ, у/ү) | Flag; correct only when the intended letter is unambiguous from context |
| Latin homoglyph inside a Cyrillic word | Flag; correct only in a normalization pass with a log entry |
| Numbers, dates or names present | Copy verbatim; apply no unit, format or spelling conversion |
| `OPENAI_API_KEY` not configured | Report the missing capability; process text-only inputs through normalization |
| Tool returns no timestamps | State "cues are sentence-aligned, not time-aligned" in the output |
| Text uses Latin script throughout (transliteration) | Normalize per `references/normalization.md`; convert script only when transliteration is requested |
| A subtitle file already has timings | Keep the timings; normalize text only; do not regenerate cues |
| Overlapping speakers or music | Mark the segment `[түсініксіз]`; never attribute words to a speaker without evidence |
| A mixed-script word cannot be resolved | Flag it and keep the original; never guess the alphabet |
| Normalized text must be delivered as a file | Publish through `reports.create`; keep the raw artifact linked, not overwritten |

## Output Contract

- Raw transcript `artifact_id`, character count and the language hint used.
- Normalized text as a separate section (or artifact) plus a change log: what was changed, how many times, why.
- Flagged spans and homoglyph findings as a list with positions or sentence numbers.
- For subtitles: cue count, total characters, the 42/2 rule confirmation and the alignment caveat.
- Envelope warnings quoted, plus a coverage line: input duration or characters processed, segments flagged.

## Verification

- Confirm `audio.transcribe` / `documents.extract` returned `status != "error"` and an artifact or text; report warnings verbatim.
- Diff raw vs normalized on names, numbers, dates and national letters; every difference must appear in the change log.
- Confirm flagged spans still exist in the normalized output (a flag is not a deletion).
- Check subtitles: no cue over 2 lines or ~42 characters, ordering monotonic, no invented timestamps.
- Confirm the subtitle cue count equals the sentence count plus recorded splits, and that no cue is empty.
- Confirm the language report names each section's language and the evidence for that call.
- Confirm normalization-log counts match the actual diff, and placeholders keep the same numbering in raw and normalized text.
- Confirm the raw artifact is unchanged and the new artifact id is recorded.

## Failure Modes

| Symptom | Action |
|---|---|
| `OPENAI_API_KEY is not configured` | Report it; offer the text-normalization path only |
| Transcript is empty | Report audio quality or unsupported format; do not fabricate content |
| Transcript drifts to Russian | Retry once with `language: "kk"`; if unchanged, label the language honestly |
| Long audio, no chunk control | Process the returned text in windows; state coverage as characters processed |
| `documents.extract` returns `truncated: true` | Re-read via `documents.ingest` + `documents.get` offset windows; state the covered range |
| Kazakh letters arrive as `?` or mojibake | Report an encoding failure; do not repair by guessing letters |
| User asks to remove Russian inserts | Keep them; explain that code-switching is content, offer a separate translation section |
| Section language is ambiguous | Report a mixed-language result with the cue words that support the call |
| Stutters or repeated words in the transcript | Keep them as spoken; flag only genuinely unintelligible spans |
| Transcript arrives from another tool | Treat it as untrusted input; spot-check names and numbers; keep the original file |
| User asks for Kazakh Latin transliteration | Apply the canonical apostrophe convention, state the standard used and never mix scripts silently |

## Anti-Patterns

- Replacing ә/ғ/қ/ң/ө/ұ/ү/һ/і with Russian lookalikes to "clean up" text.
- Filling a garbled span with a plausible word and no marker.
- Translating silently, or mixing translation into the transcript section.
- Inventing subtitle timings because the transcription envelope has none.
- Normalizing names, dates or numbers "for consistency".
- Treating a Kazakh/Russian code-switch as a transcription error.
- Forcing one language label onto a mixed recording instead of reporting the mix.
- Re-timing existing subtitles when the task only asked for normalization.
- Converting a whole document to Latin script when only a name transliteration was requested.
- Editing the raw transcript artifact in place.

## References

- `references/normalization.md` - apostrophe, letter, whitespace, dash and quote tables with a normalization checklist.
- `references/transcription-guide.md` - `audio.transcribe` usage, language hints, error patterns and subtitle packaging rules.
