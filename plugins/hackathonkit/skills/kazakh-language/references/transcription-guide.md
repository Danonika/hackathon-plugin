# Transcription Guide

How to call `audio.transcribe`, read its result honestly and package subtitles
without inventing what the envelope does not contain.

## Tool Contract

| Argument | Required | Value |
|---|---|---|
| `path` | yes | Relative path under `data/input`; absolute paths and `..` are rejected |
| `language` | no, but always send it | ISO hint: `kk` for Kazakh, `ru` for Russian |

Returns: `data.artifact_id` (stored transcript), `data.text`, `data.chars`, and
a source entry. There are **no timestamps and no per-segment confidence** in the
envelope. Do not invent either; build sentence-aligned cues and say so.

## Language Hint Table

| Audio content | Hint | Why |
|---|---|---|
| Kazakh | `kk` | Locks the decoder to Kazakh phonology |
| Russian | `ru` | Only when no Kazakh speech is present |
| Mixed kk/ru | `kk` | Keeps Kazakh primary; code-switch stays as spoken |
| Unknown | `kk` first | Retry with `ru` only if the result is clearly Russian |

## Pre-flight Checks

1. `files.describe` the input: confirm the file is audio/video and note its size.
2. Confirm `OPENAI_API_KEY` is configured; the tool fails fast when it is not.
3. If the file is a document, do not use this tool: use `documents.extract`.
4. Note the expected speaker count and topic; they help judge transcript quality.

## Flagged-Span Convention

Use a stable marker so flags survive normalization and can be counted:

```text
[түсініксіз 1] <the surrounding words>
```

- Number flags sequentially; do not add fake timestamps.
- Never replace a flag with a guessed word, even when context is strong.
- Report the flag count in the output contract.

## Subtitle Packaging

Since timings are unavailable, cues are sentence-aligned:

| Rule | Value |
|---|---|
| Characters per line | ~42 maximum |
| Lines per cue | 2 maximum |
| Cue unit | One sentence, split at clause boundaries when too long |
| Ordering | Monotonic, preserving source sentence order |
| Caveat | State "sentence-aligned, not time-aligned" in the report |

Placeholders for long sentences:

```text
1. Бүгін сағат 10:00-де өтініш береміз,
2. өйткені құжаттар дайын.
```

Do not balance line lengths artificially; keep natural clause boundaries.

## Error Patterns

| Envelope | Meaning | Action |
|---|---|---|
| `OPENAI_API_KEY is not configured` | Capability missing | Report; use the text-normalization path only |
| Empty `text` | Silent/corrupt audio or unsupported format | Report; never fabricate a transcript |
| Mostly Russian output with `kk` | Audio is Russian or heavily mixed | Retry once with `kk`; if unchanged, label the language |
| Access/path error | Path not under `data/input` | Fix the relative path; do not retry absolute paths |
| `warnings` present | Partial processing | Quote warnings in the output |

## Worked Example

Input: `data/input/demo/interview-01.m4a`, two speakers, mixed kk/ru.

```text
1. files.describe(path="demo/interview-01.m4a") -> audio, has_timestamp false
2. audio.transcribe(path="demo/interview-01.m4a", language="kk")
   -> artifact_id: audio/interview-01-20260921-101500.txt
   -> chars: 4820, warnings: []
3. Flags added: [түсініксіз 1], [түсініксіз 2]
4. Normalization log per references/normalization.md: 9 changes
5. reports.create sections: raw artifact, normalized text, log, flags,
   subtitles (37 cues, max 41 chars, sentence-aligned)
```

## Quality Checklist

- [ ] `language: "kk"` sent (unless the audio is known Russian).
- [ ] Raw artifact id recorded and left unchanged.
- [ ] Flagged spans labeled and counted; none replaced by guesses.
- [ ] Code-switching preserved.
- [ ] Subtitles obey 42/2, monotonic order, no fabricated timings.
- [ ] Sentence-alignment caveat present.
- [ ] Warnings and coverage (characters or duration processed) reported.
