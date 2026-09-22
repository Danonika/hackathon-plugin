# Kazakh Normalization Tables

Surface normalization rules for Kazakh text. Apply these after transcription or
extraction, never before: keep the raw artifact and write every change to a log.

## Canonical Forms

| Surface | Canonical | Notes |
|---|---|---|
| Apostrophe `'` U+0027 | `ʼ` U+02BC | Kazakh Latin and transliterated names |
| Right quote `’` U+2019, left quote `‘` U+2018 | `ʼ` U+02BC | Same target; do not confuse with quotation marks |
| Backtick `` ` `` U+0060, acute `´` U+00B4 | `ʼ` U+02BC | Common mobile-keyboard artifacts |
| Dash `–` U+2013, em dash `—` U+2014 | `-` or `—` | Hyphen inside words, em dash between clauses; never inside numbers |
| Quote `"` `"` U+201C/U+201D | `«` `»` | Kazakh typography uses guillemets |
| NBSP U+00A0, thin U+2009, hair U+200A | normal space U+0020 | Watch between number and unit |
| Zero-width U+200B, soft hyphen U+00AD | removed | Invisible characters break search |
| Multiple spaces / tabs | single space | Outside preformatted blocks |

## Kazakh National Letters

- Cyrillic Kazakh has 42 letters, 9 of them specific:
  **Ә ә, Ғ ғ, Қ қ, Ң ң, Ө ө, Ұ ұ, Ү ү, Һ һ, І і**.
- Never fold these to Russian letters. A fold changes meaning:
  `қ` -> `к`, `ғ` -> `г`, `ң` -> `н`, `ө` -> `о`, `ә` -> `а`, `ұ`/`ү` -> `у`,
  `һ` -> `х`, `і` -> `и`.
- The reverse error also exists: Russian `и` written where Kazakh `і` belongs,
  or `у` where `ұ`/`ү` belongs. Correct only when the word is unambiguous;
  otherwise flag it.

## Latin Homoglyphs

Mixed alphabets are common in imported strings (URLs, brand names). These
characters look identical but are different code points:

| Cyrillic | Latin lookalike | Detection |
|---|---|---|
| а е о р с х | a e o p c x | Letter is inside a word of the other script |
| у | y | Kazakh word letters around it are Cyrillic |
| В Н К М Т | B H K M T | Uppercase-only runs |

Rule: flag every mixed-script word, correct only when the intended script is
obvious, and log each correction. Never run a blind alphabet fold script.

## Spacing and Punctuation

- No space before `, . ; : ? ! )` `»`.
- One space after `, . ; : ? !` and before `«`.
- No spaces inside `«...»` or inside parentheses.
- Keep sentence case as spoken; do not title-case headings that were not headings.
- Preserve ellipsis style: either `...` or `…`, but use one of them per document.

## Numbers, Dates and Names

- Copy numbers verbatim: `5 мың`, `10 күн`, `2026 жыл` stay as written; do not
  convert to digits, ISO dates or another case.
- Keep the Kazakh date pattern when present: `2026 жылғы 15 наурыз`.
- Proper names, toponyms and abbreviations are never "corrected": if a name
  looks wrong, flag it; do not rewrite it.
- Abbreviations keep their original punctuation: `ҚР`, `Т.Ж.`, `МӘМС`.

## Code-Switching

- A Kazakh sentence containing Russian words is valid input, not an error.
- Mark switches only when the task asks for language tagging; otherwise leave
  the text as spoken.
- Never translate Russian inserts, never transliterate them to Kazakh letters.

## Normalization Log

Every change gets a row; the log is part of the output contract:

| # | Change type | Count | Example | Rule |
|---|---|---|---|---|
| 1 | Apostrophe unified | 14 | `'` -> `ʼ` | Canonical forms |
| 2 | NBSP replaced | 6 | `10 кг` -> `10 кг` | Canonical forms |
| 3 | Homoglyph corrected | 2 | Cyrillic `о` -> Latin `o` in URL | Latin homoglyphs |
| 4 | Garbled span flagged | 3 | `[түсініксіз]` | Do not guess |

## Worked Example

Raw: `Бүгін сағат 10:00-де ʼ-форматта өтініш береміз. Бағасы 5 мың теңге.`

| Step | Result |
|---|---|
| Apostrophes | `ʼ` already canonical, 0 changes |
| NBSP before `мың` | replaced, 1 change |
| Digits `10:00` | kept, no conversion |
| Flagged spans | none |
| Output | `Бүгін сағат 10:00-де ʼ-форматта өтініш береміз. Бағасы 5 мың теңге.` |

## Checklist

- [ ] Raw artifact untouched; normalized text is a new artifact.
- [ ] All nine national letters survive unchanged.
- [ ] Apostrophes unified to U+02BC; log counts match the diff.
- [ ] Invisible whitespace removed; unit spacing intact.
- [ ] Numbers, dates, names and abbreviations byte-identical to the source.
- [ ] Garbled spans flagged, not guessed.
- [ ] Code-switches preserved.
