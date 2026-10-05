# Hackathon Agent Kit — гайд для хакатона

Практическое руководство: как поставить, как работать и как показывать жюри.
Версия плагина: **0.16.92** · 41 MCP-инструмент · 29 скиллов · 1 команда · хуки · eval-подсистема.

---

## 0. Что это в двух словах

- **Codex** — основная «голова»: думает, пишет код, вызывает инструменты.
- **Плагин `hackathonkit`** — набор скиллов (методики), команда `/verify`, хуки и конфиг MCP.
- **MCP-сервер `hackathonkit-mcp`** — 41 типизированный инструмент: документы, данные, время-ряды, графики, отчёты, eval, обучение, Telegram.
- **Fly-режим** — экспериментальный роутер: подбирает инструменты под задачу и **учится на твоём фидбеке** (дофамин PAM / ошибка PPL1).
- **Демо** — **http://127.0.0.1:8787/brain-demo** (муха + мозг + метрики).

---

## 1. Что нужно на машине

| Компонент | Зачем | Проверка |
|---|---|---|
| Go 1.27+ | сборка MCP-бинаря | `go version` |
| Docker Desktop | postgres / redis / minio / demo-api | `docker info` |
| Node 20+ | Codex CLI | `node -v` |
| Codex CLI | хост-агент | `codex --version` |
| Ключ OpenAI-совместимого API | chat/vision/embeddings | в `.env` |

Ключи кладём в `.env` (создаётся из `.env.example` скриптом setup, в git не коммитится):

```
OPENAI_API_KEY=...
OPENAI_BASE_URL=https://opencode.ai/zen/go/v1     # у нас OpenCode Go → DeepSeek
POSTGRES_PASSWORD=...
MINIO_ROOT_PASSWORD=...
```

---

## 2. Установка за 5 минут

```powershell
git clone <repository-url>
cd hackathonkit
scripts\setup.ps1        # Windows  (Linux/macOS: scripts/setup.sh)
```

Что делает setup:
1. синхронизирует скиллы в зеркало `.agents/skills`;
2. собирает MCP-образ (или локальные бинари, если Docker недоступен);
3. поднимает postgres, redis, minio;
4. создаёт `.env` из `.env.example`;
5. регистрирует маркетплейс и ставит плагин:
   `codex plugin marketplace add <root>` + `codex plugin add hackathonkit@hackathonkit-local`.

Дальше:

```powershell
codex                      # запускать из корня репозитория
scripts\doctor.ps1         # проверка: рантайм, routing, плагин, скиллы, handshake MCP
scripts\smoke.ps1          # прогон демо-задачи
```

> **Важно:** плагины Codex подхватываются при старте сессии — после установки открой **новую** сессию Codex.
> Fallback без плагина: в репозитории уже есть `.codex/config.toml` (тот же MCP-сервер) и `.agents/skills`.

### Codex на DeepSeek (опционально, уже настроено)

```powershell
codex --profile deepseek                    # deepseek-v4.1-flash: 1M контекст, 384K вывод
codex --profile deepseek --model deepseek-v4-pro
```

Провайдер `opencode-go` прописан в `~/.codex/config.toml`, профиль — в `~/.codex/deepseek.config.toml`,
ключ берётся из `OPENCODE_GO_API_KEY`.

---

## 3. Рабочий цикл хакатона (главное)

```text
$task-router  →  $build-planner  →  доменные скиллы + MCP  →  /verify  →  $judge  →  freeze
```

1. **Старт задачи.** Попроси Codex: «используй `$task-router`» — он прочитает живой режим роутинга
   (`system.context`), выберет инструменты и включит правила доказательств.
2. **План.** `$build-planner` — стадии S0 (результат) → S1 (разведка) → S2 (скелет) → S3 (вертикальный срез)
   → S4 (упрочнение) → S5 (оценка) → S6 (заморозка). У каждой стадии — exit evidence и rollback.
   План хранится артефактом (`plan.create` / `plan.update` / `plan.status`).
3. **Работа.** Доменные скиллы под тему: `data-analysis`, `time-series-analysis`, `document-intelligence`,
   `rag-research`, `devops-expert`, `security-review`, `telegram-integration`, `kazakh-language` и др.
   Инструменты вызываются по мере надобности: `data.inspect` → `timeseries.detect_anomalies` → `charts.create`
   → `reports.create`.
4. **Проверка.** Команда `/verify` (плагин) прогоняет тесты и покрытие; `quality.gate` читает свежие
   доказательства (`evals/quality/<sha>/gate.json`: gofmt, build, tests). `$output-self-check` — самопроверка результата.
5. **Оценка.** `eval.freeze_bundle` — честный read-only снапшот; `$judge` — оценка по рубрике;
   `eval.aggregate` → `eval.rank` (правила 9.6, минимум 3 валидных прогона).
6. **Самоулучшение.** `$self-improvement-controller` — ограниченные раунды; решение accept/rollback/stop
   принимает только `eval.compare`.
7. **Обучение.** `learning.record` (записать урок) → `learning.recall` (вспомнить перед похожей задачей)
   → `learning.promote` (поднять в глобальные, когда повторяется).
8. **Готовность.** `$project-cost` — технический скор готовности, `$roast-project` — злой разбор перед питчем.

---

## 4. Fly-режим — фишка для жюри

**Что это.** Роутер, который ранжирует инструменты под задачу ансамблем
`keyword + embedding + kNN + MB-мозг + дофамин + LLM` и учится на обратной связи.

**Включить:** тумблер Normal/Fly на дашборде `/` (или `POST /routing {"mode":"fly"}`).

**Цикл:**

```powershell
# 1. рекомендация под задачу
'{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"routing.fly_recommend","arguments":{"task":"find anomalies in sensor telemetry"}}}' | bin\hackathonkit-mcp.exe --repo-root .

# 2. финальное решение (награда — PAM, исправление — PPL1)
'{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"routing.fly_feedback","arguments":{"task":"find anomalies in sensor telemetry","chosen":["timeseries.detect_anomalies"],"source":"accepted"}}}' | bin\hackathonkit-mcp.exe --repo-root .
```

**Что происходит:** PAM усиливает выбранные связи, PPL1 ослабляет ошибочные; веса пишутся в
`data/index/fly-brain.json`, события — в `logs/fly-neuron-events.jsonl`, страницы подхватывают их по SSE `/fly-live`.

**Обучение мозга:**

```powershell
go run ./cmd/fly-train --model mb --dataset data/fly-dataset.jsonl --epochs 30   # переобучить MB
go run ./cmd/fly-train --model knn                                                # перестроить память из фидбека
```

---

## 5. Демо для жюри

**Демо-страница (одна):** **`http://127.0.0.1:8787/brain-demo`** — муха + мозг + метрики:
стимуляция PAM/PPL1, живые события, телеметрия, 3D-модель.

**Сценарий на 2 минуты:**
1. Открыть `/brain-demo` заранее (первая загрузка мешей ~15 с) и прогреть.
2. «Награда (PAM)» → муха встаёт, хлопает, мозг зеленеет, график даёт пик.
3. «Ошибка (PPL1)» → красный пульс, другая реплика мухи.
4. Живое событие: прогнать `routing.fly_recommend` + `routing.fly_feedback` (команды выше) —
   мозг отреагирует сам, это реальный цикл, а не анимация.

**Регрессия демо:** `scripts\e2e-fly.ps1` — 25 проверок (страницы, роутинг, награда/боль, события, память).

**Честность перед жюри:** меши flybody/VFB — реальные (Apache-2.0/MIT), обучение и дофамин — настоящие
артефакты; «Spikes/s» — оценка по активным KC (подписано в UI); эмбеддинги — локальный hash-фолбэк,
если у провайдера нет `/v1/embeddings`.

---

## 6. Шпаргалка по MCP-инструментам

| Группа | Инструменты |
|---|---|
| Система/роутинг | `system.context`, `system.capabilities`, `routing.fly_recommend`, `routing.fly_feedback`, `routing.variant`, `system.resources`, `system.resources_set`, `system.resources_probe`, `system.context_budget` |
| Оценка | `eval.freeze_bundle`, `eval.aggregate`, `eval.calibrate`, `eval.compare`, `eval.rank` |
| Обучение | `learning.record`, `learning.recall`, `learning.promote` |
| Планы | `plan.create`, `plan.update`, `plan.status` |
| Качество/безопасность | `quality.gate`, `security.review` |
| Telegram | `telegram.send`, `telegram.validate`, `telegram.manage` |
| Файлы/документы | `files.describe`, `documents.ingest`, `documents.extract`, `documents.get` |
| Знания | `knowledge.index`, `knowledge.search` |
| Данные/время-ряды | `data.inspect`, `data.query`, `data.analyze`, `timeseries.analyze`, `timeseries.detect_anomalies`, `classification.classify` |
| Вывод/медиа | `charts.create`, `reports.create`, `audio.transcribe`, `vision.analyze` |

---

## 7. Скрипты

| Скрипт | Назначение |
|---|---|
| `scripts\setup.ps1` | установка «с нуля» |
| `scripts\doctor.ps1` | диагностика окружения и handshake MCP |
| `scripts\smoke.ps1` | дымовой прогон демо-задачи |
| `scripts\repair.ps1` | починить типовые поломки |
| `scripts\plugin-check.ps1` | проверка плагина и скиллов |
| `scripts\e2e-fly.ps1` | сквозной E2E Fly (25 проверок) |
| `scripts\sync-skills.ps1` | пересобрать зеркало скиллов |
| `scripts\docker-check.sh` · `quality-gate.sh` | Linux/macOS-варианты |

---

## 8. Грабли и как их обойти

| Симптом | Причина / решение |
|---|---|
| Плагин «не видно» в Codex | открыть **новую** сессию Codex; проверить `codex plugin list` |
| `docker: cannot connect` | Docker Desktop закрыт — запустить и подождать 30–60 с |
| Страница показывает старое | жёсткое обновление `Ctrl+Shift+R` |
| Демо долго грузится | 136 МБ мешей мухи — прогреть страницы до выхода жюри |
| Порт 8787 занят | `HACKATHONKIT_DEMO_PORT` в `.env` или остановить старый контейнер |
| MCP не отвечает | `scripts\doctor.ps1`; пересобрать: `go build -o bin\hackathonkit-mcp.exe ./cmd/hackathonkit-mcp` |
| Ключ засветился | ротировать после хакатона, `.env` не коммитить |

---

## 9. Чеклист за 15 минут до питча

- [ ] `scripts\doctor.ps1` — без ошибок
- [ ] `docker compose ps` — все сервисы healthy
- [ ] прогреть `/brain-demo` в браузере
- [ ] `scripts\e2e-fly.ps1` → `ALL CHECKS PASSED`
- [ ] под рукой команды «живого события» (раздел 4)
- [ ] `$judge` прогнан на замороженном снапшоте (`eval.freeze_bundle`)
- [ ] ключи к API ротированы после выступления
