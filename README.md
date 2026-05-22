# vks4 — Production-grade MCU видеоконференц-сервер

Микширующий (MCU) видеоконференц-сервер корпоративного уровня.

- **WebRTC** клиенты (Chromium / Firefox / Safari).
- **SIP / H.323** через FreeSWITCH-edge (gateway-подход).
- **Серверное микширование** audio/video через GStreamer.
- **Admin SPA**: управление комнатами, layout editor, RBAC, мониторинг, recordings.
- **Observability**: Prometheus + Grafana + Loki.
- **Готов к Kubernetes**, но MVP запускается одним `docker compose up -d`.

См. подробный план в [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) и
[`docs/MEDIA_PIPELINE.md`](docs/MEDIA_PIPELINE.md).

## Quick start

```bash
cp .env.example .env
make gen          # генерация OpenAPI, sqlc, buf
make docker       # сборка образов
make up           # docker compose up -d
make migrate-up
```

Открыть https://localhost/ (Caddy с локальным самоподписанным TLS),
вход: `admin@local` / `admin` (из seed).

## Структура

| Каталог | Содержимое |
|---|---|
| `services/control-plane` | REST API, RBAC, rooms, recordings |
| `services/signaling`     | WebSocket-сигналинг |
| `services/media-router`  | Dispatcher: room → media-worker |
| `services/media-worker`  | Pion + GStreamer compositor / mixer |
| `services/sip-gateway`   | Мост FreeSWITCH ↔ MCU (каркас) |
| `web/admin`              | React + Vite admin SPA |
| `deploy/`                | Caddy, coturn, Prometheus, Loki, Grafana, FreeSWITCH |
| `api/`                   | OpenAPI + protobuf (source of truth) |
| `tools/load-tester`      | Эмулятор виртуальных WebRTC-peer-ов |
| `docs/`                  | Архитектура, deploy, security, roadmap |

## Состояние первой итерации

- [x] Архитектурный скелет всего репозитория.
- [x] Working WebRTC ↔ WebRTC MCU (audio + video mixing) на одной ноде.
- [x] Полный admin SPA со всеми экранами ТЗ.
- [x] Observability stack.
- [x] docker-compose + Caddy + TLS.
- [ ] Реальный SIP-bridge через FreeSWITCH — каркас и интерфейсы готовы, runtime — v0.2.
- [ ] H.323 — gateway-подход через FreeSWITCH, в roadmap.

См. [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Лицензия

MIT (заглушка, замените на свою при коммерческом использовании).
Внимание: `x264` / FreeSWITCH-стек требуют отдельной проверки лицензий
при коммерческом деплое — см. [`docs/SECURITY.md`](docs/SECURITY.md).
