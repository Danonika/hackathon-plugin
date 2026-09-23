# Domain Skill Routing

Pick the minimal set of skills for the current stage. More than two domain
skills in one stage usually means the stage is too large: split it.

| Signal | Skill | First tools |
|---|---|---|
| PDF/scanned document, extraction, classification | document-intelligence | `documents.ingest`, `documents.extract`, `documents.get` |
| CSV/TSV/XLSX table, profiling, filtering, joins | data-analysis | `files.describe`, `data.inspect`, `data.query` |
| Time-series telemetry, anomalies, resampling | time-series-analysis | `data.inspect`, `timeseries.analyze`, `timeseries.detect_anomalies` |
| Question over `data/input` | rag-research | `documents.ingest`, `knowledge.index`, `knowledge.search` |
| Government form, procedure, regulation citation | government-services | `knowledge.search`, `reports.create` |
| Kazakh text, transcription, localization | kazakh-language | `audio.transcribe`, `documents.extract` |
| Final report or brief | report-builder | `charts.create`, `reports.create` |
| Notifications, project bot | telegram-integration | `telegram.validate`, `telegram.send`, `telegram.manage` |
| Docker/compose stack for the built project | devops-expert | `files.describe`, quality gate |

## Code changes

When the stage writes or changes code, run the review pair **in this order**:

1. `$cp-expert-refactoring` - hot paths, algorithmic and performance, with
   before/after measurements.
2. `$system-design-expert` - module boundaries, contracts, failure isolation.

Apply only changes with a measured benefit; keep public contracts stable.
