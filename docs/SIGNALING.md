# Signaling protocol

Клиент ↔ signaling сервер через WebSocket. Endpoint: `wss://<host>/ws`,
subprotocol `vks4.signaling.v1`. Все сообщения — JSON `{type, payload}`.

## Аутентификация

Клиент получает JWT через `POST /api/v1/auth/login`. JWT прикладывается в первом
сообщении `join`. Без валидного JWT сервер закрывает соединение с `error:unauthorized`.

## Поток сообщений

```
client → server                          server → client
─────────────────────────────────────────────────────────
{ type:"join", payload:{roomId,token,displayName} }
                                           { type:"joined", payload:{selfId, participants[]} }
                                           { type:"peer-joined", payload:Participant }     (бродкаст всем)
{ type:"offer", payload:{sdp} }
                                           { type:"answer", payload:{sdp} }
{ type:"candidate", payload:{candidate,sdpMid,sdpMLineIndex} }
                                           { type:"candidate", payload:{...} }              (от media-worker)
                                           { type:"active-speaker", payload:{peerId} }
                                           { type:"layout", payload:{mode, grid?} }
                                           { type:"stats", payload:{rtt,jitter,loss,bitrate} }
{ type:"chat", payload:{text} }
                                           { type:"chat", payload:{text, from} }            (бродкаст)
{ type:"control", payload:{action,data} }
                                           { type:"control", payload:{...} }                (бродкаст)
{ type:"leave" }                           
                                           { type:"peer-left", payload:Participant }
{ type:"ping" }                            { type:"pong" }
```

## Ошибки

`{ type:"error", payload:{ code, message } }` — `unauthorized`, `invalid_argument`,
`not_found`, `forbidden`, `internal`.

## Control actions

| action | payload | роль |
|---|---|---|
| `mute` / `unmute` | `{peerId}` | self / moderator |
| `videoOff` / `videoOn` | `{peerId}` | self / moderator |
| `raiseHand` | `{}` | self |
| `setLayout` | `{mode}` | moderator / host |
| `setPresenter` | `{peerId}` | moderator / host |
| `kick` | `{peerId}` | moderator |
