# FreeSWITCH config (skeleton)

В первой итерации FreeSWITCH используется только как edge-узел для SIP/H.323 в режиме gateway.
Поднимается через профиль `sip` в docker-compose:

```
docker compose --profile sip up -d freeswitch sip-gateway
```

Здесь должна лежать минимальная конфигурация:

- `freeswitch.xml` — root
- `sip_profiles/external.xml` — внешний профиль (TLS, SRTP)
- `dialplan/public.xml` — маршрутизация входящих звонков на sip-gateway
- `autoload_configs/event_socket.conf.xml` — ESL для sip-gateway

Заполняется в v0.2 (см. `docs/ROADMAP.md`).

> ⚠️ Образ `signalwire/freeswitch:1.10` уже содержит ванильную конфигурацию.
> Если не монтировать ничего, FreeSWITCH стартует с дефолтами для отладки.
