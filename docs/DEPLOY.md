# Deploy

## Локально / dev

```bash
git clone <repo>
cd vks4
cp .env.example .env
make keys                 # сгенерировать JWT RS256 ключи в ./secrets/
make docker               # сборка всех образов
make up                   # docker compose up -d
make migrate-up
```

Открыть https://localhost/ → войти `admin@local` / `admin`.

Caddy выдаёт self-signed TLS через свой local CA. Для доверия — добавьте корневой
сертификат в систему (см. `caddy trust`).

## Production (Docker Compose)

1. Зарегистрируйте домен и направьте A/AAAA на сервер.
2. В `deploy/caddy/Caddyfile` замените `:443 { tls internal }` на:
   ```
   vks4.example.com {
       tls ops@example.com
       …
   }
   ```
3. Откройте порты `80/tcp`, `443/tcp`, `3478/udp` (coturn STUN), `49152-65535/udp` (TURN relay).
4. Скопируйте `secrets/jwt_{private,public}.pem` сгенерированные `make keys`.
5. Запустите:
   ```bash
   docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
   ```

### TURN

В prod coturn должен быть доступен извне и иметь публичный IP в `external-ip` файла
конфигурации. Авторизация — через `use-auth-secret` + `static-auth-secret` или REST API.

## Kubernetes (roadmap)

Helm chart — в roadmap v0.5. Минимальный набор манифестов:

- 1× Deployment + Service для каждого Go-сервиса (HPA по CPU).
- 1× StatefulSet для media-worker (sticky по `room_id`).
- 1× DaemonSet для coturn (host networking).
- PersistentVolume для Postgres + MinIO.

## Backup / DR

- Postgres: `pg_basebackup` ежедневно + WAL-archive в S3.
- MinIO: bucket replication на удалённый кластер.
- Redis: AOF включён; ребут безопасен (presence просто пересоберётся).

## Обновления

1. `git pull && make docker`.
2. `docker compose up -d --no-deps --build <service>` — постепенно по сервисам.
3. После всех Go-сервисов — `make migrate-up`.
4. Финально — `admin-web`.
