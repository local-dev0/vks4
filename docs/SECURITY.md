# Security

## Принципы

- **Secure by default**: вся сетевая поверхность за Caddy + TLS.
  Service-to-service — внутри docker network.
- **Принцип минимальных привилегий**: каждый сервис под non-root user в контейнере,
  read-only FS (опционально через docker-compose `read_only: true`).
- **Никаких секретов в образах**: всё через `.env` + bind-mount в `/run/secrets`.

## Auth

- JWT RS256 (ключи 2048+). Access token TTL — 15m, refresh — 30d, rotation on use.
- Refresh-tokens хранятся в Postgres с возможностью revoke.
- В roadmap — OIDC через Keycloak: replace JWT issuer, sync ролей.

## RBAC

| Роль | Что доступно |
|------|--------------|
| `admin` | Всё, включая users, audit, settings |
| `operator` | Создание/изменение/удаление комнат, rec, kick |
| `moderator` | Kick, mute, layout change в рамках комнаты |
| `viewer` | Read-only списки комнат, recordings, monitoring |

Enforce в middleware (`RequireRole`) + в DB constraints.

## Транспорт

- **TLS 1.3** на edge (Caddy auto-issued Let's Encrypt в prod).
- **DTLS-SRTP** для всех WebRTC peer-connections (mandatory, fallback запрещён).
- **SRTP** для FreeSWITCH-edge SIP-каналов (SIPS + sRTP profile в `sofia.conf`).
- HSTS + CSP + XFO + Referrer-Policy на admin-зоне (см. Caddyfile).
- Strict CORS (whitelist origins, не `*` в prod).

## Хранение

- Пароли — `bcrypt` (cost=10).
- Audit log — append-only с `id` (uuid) + `created_at`. Удаление запрещено
  на уровне приложения; внешний WORM-bucket (MinIO + ObjectLock) в roadmap.
- Recordings — S3-объекты с server-side encryption (`SSE-S3`).

## Дополнительная защита

- Rate limiting на `/auth/login`, `/auth/refresh` через `httprate`.
- IP-логирование с x-forwarded-for валидацией (только от Caddy).
- CSP `default-src 'self'`, без inline scripts. Если потребуется
  Grafana iframe — добавить хост в `frame-src`.
- Dependency scanning в CI: `govulncheck`, `npm audit`, `trivy` на образы.

## Известные риски

- **H.264 лицензия**: x264 в коммерческом деплое требует MPEG-LA лицензии.
  По умолчанию используем VP8/VP9; H.264 опционален.
- **FreeSWITCH** в edge режиме унаследовал свой attack surface — изолируйте
  его dedicated сетью; не открывайте RTP-диапазон шире необходимого.
- **TURN open relay**: НЕ используйте `use-auth-secret` без ротации; абьюз
  ведёт к биллингу за исходящий трафик.
