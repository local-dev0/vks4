# Roadmap

## v0.1 (MVP, текущая итерация)

- [x] Архитектурный скелет всего репозитория, clean architecture в каждом Go-сервисе.
- [x] Working WebRTC ↔ WebRTC MCU: ingress, GStreamer compositor + audiomixer, egress.
- [x] REST API: auth + JWT, rooms, layouts, participants, recordings, users, RBAC, audit.
- [x] WebSocket signaling, room hub, бродкаст событий.
- [x] Admin SPA (React + Vite): все экраны из ТЗ.
- [x] docker-compose + Caddy + coturn + Postgres + Redis + MinIO.
- [x] Observability: Prometheus + Grafana + Loki + promtail.
- [x] sip-gateway каркас (ESL клиент + bridge stub).

## v0.2

- [ ] Реальный SIP-bridge: FreeSWITCH ↔ MCU через ESL события + mod_audio_fork.
- [ ] Waiting room: модератор пускает участников из очереди.
- [ ] DTMF события из RTC и SIP → IVR-логика.
- [ ] OIDC через Keycloak (вместо bootstrap admin@local).
- [ ] Тонкая настройка adaptive bitrate (TWCC reaction loop).

## v0.3

- [ ] H.323 gateway через FreeSWITCH mod_h323.
- [ ] Recording → HLS preview для admin (segmented mp4 → HLS).
- [ ] Embedded i18n (en, ru, es) с полноценной локализацией админки.
- [ ] Drag-and-drop layout editor: snap-to-grid, undo/redo, шаблоны.

## v0.4

- [ ] Simulcast / SVC egress (1080p + 720p + 360p из одного pipeline).
- [ ] GPU encoding: `nvh264enc` (NVENC) / `vah264enc` (VAAPI) — 10× плотности.
- [ ] Live migration комнаты между worker-ами (для rolling upgrade без disconnect).
- [ ] PHP-FPM-style worker pool для media-worker с reload signal.

## v0.5

- [ ] Kubernetes Helm chart + operator (rooms — CRD).
- [ ] Multi-region deploy: federated TURN, anycast edge.
- [ ] Vault / sealed-secrets для prod секретов.
- [ ] OpenTelemetry tracing через Tempo/Jaeger.

## v0.6

- [ ] E2EE для приватных комнат (Insertable Streams / Frame Encryption).
- [ ] End-to-end auth для SIP/H.323 trunks через короткоживущие токены.
- [ ] Audit log в WORM-bucket с object lock.

## v0.7

- [ ] AI-функции:
  - speech-to-text live captions (whisper.cpp на media-worker side-car).
  - noise suppression (RNNoise / DeepFilter).
  - face framing / virtual background.

## v0.8

- [ ] Mobile SDK: React Native / Flutter обвязка вокруг libwebrtc.
- [ ] Recording analytics: участники, speaker time, slides cut.
- [ ] Operator dashboard: realtime QoS heat-map, alert routing в PagerDuty.
