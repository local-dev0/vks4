# Media pipeline

## Цель

Серверное микширование (MCU) аудио и видео N участников в один общий микс
с динамической раскладкой и трансляцией обратно каждому участнику.

## Структура

```
[Pion ingress]
   peer1.video (RTP) ──▶ appsrc-v1 ─▶ rtpvp8depay ─▶ vp8dec ─▶ videoconvert ─▶ videoscale
                                                                                       ╲
   peer2.video (RTP) ──▶ appsrc-v2 ─▶ ...                                              ─▶ compositor (vmix)
                                                                                       ╱
                                                                                      …
                                            vmix.src ─▶ tee ─┬─▶ encoder ─▶ rtpXpay ─▶ appsink (egress)
                                                            └─▶ valve ─▶ mp4mux ─▶ filesink (recording)

[Pion ingress, audio]
   peer1.audio (RTP) ──▶ appsrc-a1 ─▶ rtpopusdepay ─▶ opusdec ─▶ audioconvert
                                                                            ╲
   peer2.audio (RTP) ──▶ appsrc-a2 ─▶ …                                     ─▶ audiomixer (amix)
                                                                            ╱
                                            amix.src ─▶ tee ─┬─▶ opusenc ─▶ rtpopuspay ─▶ appsink
                                                            └─▶ voaacenc ─▶ mp4 (mux в общем filesink)
```

## Динамика

- При `add_peer` создаются два `appsrc` (video, audio) и request-pad на vmix/amix.
- Layout engine задаёт `xpos / ypos / width / height / alpha / zorder` на каждом pad.
- При `remove_peer` request-pad-ы освобождаются, queue дренируется.
- На каждом аудио-входе крепится `level` элемент: его сообщения вызывают `asd.Push`.

## ASD (active speaker detection)

- Окно: 1 секунда.
- Порог: −45 dBFS.
- Гистерезис: 300 мс — пока тот же спикер удерживает лидерство.
- При смене → событие `active-speaker` в room event bus → layout engine + WS клиентам.

## Adaptive bitrate

- Pion interceptor собирает REMB/TWCC от клиента.
- При снижении доступной полосы → `g_object_set(encoder, "bitrate", new)` в pipeline.
- Также уменьшается FPS через `videorate` при критической нагрузке (planned v0.4).

## Recording

- `tee` ответвляет от mixer, через `valve` (на/выкл записи), `mp4mux ! filesink`.
- При остановке файл закрывается, размер замеряется, опционально загружается в MinIO.
- Сегментацию (split каждые N минут) можно включить через `splitmuxsink` (v0.3).

## Кодеки

| Тип | Ingress | Compose | Egress | Файл | Прим. |
|-----|---------|---------|--------|------|-------|
| Video | VP8/VP9/H.264 simulcast | raw I420 | VP8 (default) / H.264 | H.264 в mp4 | H.264 лицензия — см. SECURITY.md |
| Audio | Opus | PCM S16LE 48kHz stereo | Opus | AAC в mp4 | |

## Производительность

- Одна live комната ~ 720p30 mix → 1 vCPU.
- 25 параллельных комнат на 32 vCPU node — реалистичная цель MVP.
- GPU encoder (`nvh264enc`/`vah264enc`) — 10×, в roadmap.
