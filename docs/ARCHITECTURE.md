# Architecture

vks4 — модульный MCU (Multipoint Control Unit) видеоконференц-сервер с серверным
микшированием. Дизайн заточен под clean architecture, горизонтальное масштабирование
по комнатам и production-ready DevOps (Docker / Kubernetes-ready).

## Высокоуровневая схема

```
   WebRTC ──┐
   SIP   ───┼─▶ Caddy (TLS) ──┬─▶ control-plane (REST API + RBAC + audit)
   H.323 ──┘                  ├─▶ signaling   (WebSocket, SDP nego)
                              ├─▶ admin SPA   (React)
                              ├─▶ /grafana    (embedded)
                              └─▶ /loki       (logs proxy)
                                  │
                                  ▼
                          media-router (sticky room→worker)
                                  │
                                  ▼
                          media-worker (Pion + GStreamer)
                                  │ (TURN/STUN — coturn)
                                  ▼
                                  RTP/SRTP к участникам

Stateful:                  Postgres, Redis, MinIO
Observability:             Prometheus, Grafana, Loki, promtail
SIP/H.323 edge (опц.):     FreeSWITCH (профиль `sip`)
```

## Сервисы

| Сервис | Состояние |
|---|---|
| `control-plane` | REST API, JWT, RBAC, rooms, recordings, audit. Stateless (state в Postgres + Redis). |
| `signaling`     | WebSocket-сигналинг, room hub, бродкаст событий. Stateless. |
| `media-router`  | Sticky выбор worker для комнаты. Stateless, читает Redis. |
| `media-worker`  | Stateful per-room (Pion peerconnections + GStreamer pipeline). |
| `sip-gateway`   | Каркас bridge FreeSWITCH ↔ MCU (v0.2). |
| `admin-web`     | React SPA, статически отдаётся через nginx за Caddy. |

## Принципы кода

- **Clean architecture**: `cmd/ → app → usecase → domain ← adapters`.
- **Source of truth контрактов**: `api/openapi.yaml` (REST) и `api/proto/*.proto` (gRPC).
  Все клиенты генерируются (`make gen`).
- **Конфиг**: только через env (12-factor). Секреты — через файлы в `/run/secrets`.
- **Логи**: structured JSON через `zap` (Go) и pino-friendly формат на nginx.
- **Метрики**: Prometheus на `/metrics:9100` каждого сервиса.
- **Healthchecks**: HTTP `/health` + Docker `HEALTHCHECK`.

## Поток данных WebRTC ↔ WebRTC

1. Клиент → `POST /api/v1/auth/login` → JWT.
2. Клиент → `GET /api/v1/rooms/{id}` → room.joinUrl.
3. Клиент → `wss://…/ws` → отправляет `{type:"join", payload:{roomId, token}}`.
4. `signaling` парсит JWT, вызывает `media-worker /v1/rooms/{room}/peers` с SDP offer.
5. `media-worker` создаёт `webrtc.PeerConnection`, отправляет answer обратно.
6. `signaling` пересылает answer клиенту.
7. RTP-трафик идёт **напрямую** между клиентом и media-worker (через coturn при NAT).
8. media-worker депейлоадит RTP, пушит в GStreamer appsrc; compositor микширует;
   encoder RTP-пейлоадит; media-worker раздаёт пакеты всем peer-tracks комнаты.

## SIP / H.323 (v0.2)

1. INVITE приходит в FreeSWITCH.
2. FS отправляет event в sip-gateway через ESL (`CHANNEL_CREATE`).
3. sip-gateway создаёт виртуальный WebRTC peer в media-worker (через тот же `/peers` API)
   и проксирует RTP через `mod_audio_fork` / `mod_verto`.
4. Звук участника микшируется в общую комнату; обратно — egress audio mix → SIP-канал.
5. H.323 терминируется тем же FS (mod_h323) или внешним H.323↔SIP bridge-ом.

## Масштабирование

- Один media-worker процесс держит N комнат до CPU-предела (encoder bound).
- Размещение: один room → один worker (consistent hashing на media-router).
- БД: PostgreSQL primary + read-replicas, pgbouncer.
- Redis Sentinel/Cluster в prod.
- Стейт комнаты восстанавливается из БД + Redis; падение worker = переподключение клиентов.
- В v0.4 — GPU-encoding (NVENC/VAAPI) для 10× плотности.

См. `docs/MEDIA_PIPELINE.md` для деталей GStreamer pipeline.
