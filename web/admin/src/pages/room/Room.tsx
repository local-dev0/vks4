import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Mic, MicOff, Video as VideoIcon, VideoOff, PhoneOff } from "lucide-react";
import { getAccess } from "@/lib/api";
import { useAuth } from "@/features/auth/AuthProvider";
import { RoomsApi, type Layout, type Room as RoomDTO } from "@/features/rooms/api";
import type { Envelope } from "@/lib/ws";

const ICE: RTCIceServer[] = [
  { urls: ["stun:62.238.21.186:3478"] },
];

type ConnState = "init" | "joining" | "connected" | "failed" | "closed";

const defaultLayout: Layout = { mode: "grid" };

export function Room() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const { user } = useAuth();

  const localRef = useRef<HTMLVideoElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const pcRef = useRef<RTCPeerConnection | null>(null);

  const [state, setState] = useState<ConnState>("init");
  const [error, setError] = useState<string | null>(null);
  const [muted, setMuted] = useState(false);
  const [videoOff, setVideoOff] = useState(false);
  const [layout, setLayout] = useState<Layout>(defaultLayout);
  const [room, setRoom] = useState<RoomDTO | null>(null);
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  const [reconnecting, setReconnecting] = useState(false);
  const reconnectCountRef = useRef(0);
  const startRef = useRef<() => Promise<void>>(async () => {});
  const connectedAtRef = useRef<number>(0);
  // MCU output: server отдаёт микшированный кадр в slot-0 (stream.id="slot-0").
  // Дополнительные slot-1..slot-3 пока зарезервированы для будущих фич (свободные камеры).
  const [slotStreams, setSlotStreams] = useState<Record<string, MediaStream>>({});
  // roster: server присылает массив {slot, peerId, displayName} — кто из пиров в каком слоте.
  // Используется для UI-индикаторов и для редактора (peer pinning).
  const [roster, setRoster] = useState<Array<{ slot: number; peerId: string; displayName: string }>>([]);

  const displayName = useMemo(() => user?.email ?? "guest", [user]);

  // Получаем текущий layout комнаты при заходе.
  useEffect(() => {
    if (!id) return;
    RoomsApi.get(id)
      .then((r) => {
        setRoom(r);
        if (r.defaultLayout) setLayout(r.defaultLayout);
      })
      .catch((e) => setError(`load room: ${(e as Error).message}`));
  }, [id]);

  const start = useCallback(async () => {
    setError(null);
    setState("joining");

    const token = getAccess();
    if (!token) { setError("Войдите в систему сначала"); setState("failed"); return; }

    let local: MediaStream;
    try {
      // Ограничиваем разрешение/fps для экономии bandwidth (камеры по умолчанию шлют 1080p@30
      // что даёт ~2-3 Mbps uplink). 640×480@24 хватает MCU-микшеру и режет трафик до ~400-700 kbps.
      local = await navigator.mediaDevices.getUserMedia({
        video: {
          width: { ideal: 640, max: 1280 },
          height: { ideal: 480, max: 720 },
          frameRate: { ideal: 24, max: 30 },
        },
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      });
    } catch (e) {
      setError(`getUserMedia: ${(e as Error).message}`);
      setState("failed");
      return;
    }
    setLocalStream(local);
    if (localRef.current) localRef.current.srcObject = local;

    const pc = new RTCPeerConnection({ iceServers: ICE });
    pcRef.current = pc;

    pc.ontrack = (ev) => {
      // Каждый track приходит в своём MediaStream с stream.id="slot-N".
      // slot-0 — это MCU-микс с сервера (видео+аудио). Регистрируем все, slot-0 повесим на mcuRef.
      ev.streams.forEach((s) => {
        setSlotStreams((prev) => {
          if (prev[s.id]) {
            // Stream того же id уже есть — добавим track в него (audio+video приходят отдельно).
            s.getTracks().forEach((t) => {
              if (!prev[s.id].getTracks().some((x) => x.id === t.id)) prev[s.id].addTrack(t);
            });
            return prev;
          }
          return { ...prev, [s.id]: s };
        });
      });
    };

    pc.onconnectionstatechange = () => {
      const s = pc.connectionState;
      if (s === "connected") {
        connectedAtRef.current = Date.now();
        setState("connected");
      }
      if (s === "failed" || s === "disconnected") setState("failed");
      if (s === "closed") setState("closed");
    };

    local.getTracks().forEach((t) => pc.addTrack(t, local));

    // Ограничиваем outgoing video bitrate. По умолчанию Chrome шлёт до 2.5 Mbps,
    // что слишком жирно для MCU-микса где peer становится крошечной ячейкой.
    // 500 kbps достаточно для 480p при норм. качестве.
    const videoSender = pc.getSenders().find((s) => s.track?.kind === "video");
    if (videoSender) {
      const params = videoSender.getParameters();
      if (!params.encodings || params.encodings.length === 0) params.encodings = [{}];
      params.encodings[0].maxBitrate = 500_000;
      params.encodings[0].maxFramerate = 24;
      try {
        await videoSender.setParameters(params);
      } catch (e) {
        console.warn("setParameters video", e);
      }
    }

    // Сервер заводит 1 sendonly video (MCU output, slot-0) и 20 sendonly audio
    // (по одному на каждый peer-slot для SFU mix-minus). Клиент должен добавить
    // соответствующие recvonly transceivers ДО createOffer чтобы SDP согласовалась.
    pc.addTransceiver("video", { direction: "recvonly" });
    const AUDIO_SLOT_COUNT = 20;
    for (let i = 0; i < AUDIO_SLOT_COUNT; i++) {
      pc.addTransceiver("audio", { direction: "recvonly" });
    }

    const wsProto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${wsProto}//${location.host}/ws`, "vks4.signaling.v1");
    wsRef.current = ws;

    ws.addEventListener("open", async () => {
      ws.send(JSON.stringify({ type: "join", payload: { roomId: id, token, displayName } }));
    });

    ws.addEventListener("message", async (msg) => {
      const env: Envelope = JSON.parse(msg.data);
      try {
        if (env.type === "joined") {
          const offer = await pc.createOffer();
          await pc.setLocalDescription(offer);
          ws.send(JSON.stringify({ type: "offer", payload: { sdp: offer.sdp } }));
        } else if (env.type === "answer") {
          const p = env.payload as { sdp: string };
          await pc.setRemoteDescription({ type: "answer", sdp: p.sdp });
        } else if (env.type === "layout") {
          // Реальное применение layout из админки.
          const p = env.payload as Layout | undefined;
          if (p && p.mode) setLayout(p);
        } else if (env.type === "roster") {
          // Slot mapping от сервера: какой peer занимает какой slot.
          const p = env.payload as Array<{ slot: number; peerId: string; displayName: string }>;
          if (Array.isArray(p)) setRoster(p);
        } else if (env.type === "error") {
          const p = env.payload as { code: string; message: string };
          setError(`${p.code}: ${p.message}`);
          setState("failed");
        }
      } catch (e) {
        setError(`signaling: ${(e as Error).message}`);
        setState("failed");
      }
    });

    ws.addEventListener("close", () => {
      if (state !== "failed") setState("closed");
    });
  }, [id, displayName, state]);

  // Сохраняем актуальную ссылку на start, чтобы reconnect мог её вызвать.
  useEffect(() => { startRef.current = start; }, [start]);

  // Перезаход в комнату при freeze: чистим текущие соединения и снова запускаем start().
  const reconnect = useCallback(async () => {
    if (reconnecting) return;
    setReconnecting(true);
    reconnectCountRef.current += 1;
    // Закрываем WS и pc, ОСТАВЛЯЕМ camera/mic stream (его пересоздание просит permission заново).
    try { wsRef.current?.close(); } catch {}
    try { pcRef.current?.getSenders().forEach((s) => s.track?.stop()); } catch {}
    try { pcRef.current?.close(); } catch {}
    // Останавливаем старый localStream (start() возьмёт getUserMedia заново — Chrome не спросит).
    if (localStream) localStream.getTracks().forEach((t) => t.stop());
    setLocalStream(null);
    setSlotStreams({});
    setRoster([]);
    pcRef.current = null;
    wsRef.current = null;
    setState("init");
    // Небольшая пауза чтобы ICE/UDP сокеты освободились.
    await new Promise((r) => setTimeout(r, 800));
    setReconnecting(false);
    await startRef.current();
  }, [reconnecting, localStream]);

  // Freeze detector ВРЕМЕННО ОТКЛЮЧЁН: пока MCU pipeline отдаёт черный кадр на старте,
  // detector ошибочно срабатывает и зацикливает reconnect. Включим обратно когда pipeline
  // стабильно выдаёт настоящие frames.
  useEffect(() => {
    if (state !== "connected") return;
    // no-op
  }, [state, reconnect]);

  // Disconnect-handler: даём 5 сек на восстановление ICE.
  // Этот useEffect просто переустанавливает callback; основной callback стоит в start().
  useEffect(() => {
    const pc = pcRef.current;
    if (!pc) return;
    if (state !== "connected") return;
    let timer: number | undefined;
    const orig = pc.onconnectionstatechange;
    pc.onconnectionstatechange = (ev) => {
      // вызовем оригинальный callback из start()
      if (orig) (orig as (e: Event) => unknown).call(pc, ev as Event);
      const s = pc.connectionState;
      if (s === "disconnected") {
        timer = window.setTimeout(() => {
          if (pc.connectionState !== "connected") {
            console.warn("[Room] ICE not recovered, reconnecting");
            reconnect();
          }
        }, 5000);
      } else if (timer) {
        clearTimeout(timer);
        timer = undefined;
      }
      if (s === "failed") reconnect();
    };
    return () => { if (timer) clearTimeout(timer); };
  }, [state, reconnect]);

  const leave = useCallback(() => {
    wsRef.current?.close();
    pcRef.current?.getSenders().forEach((s) => s.track?.stop());
    pcRef.current?.close();
    if (localRef.current?.srcObject instanceof MediaStream) {
      localRef.current.srcObject.getTracks().forEach((t) => t.stop());
    }
    setState("closed");
    nav(`/rooms/${id}`);
  }, [nav, id]);

  useEffect(() => () => {
    wsRef.current?.close();
    pcRef.current?.close();
  }, []);

  function toggleMute() {
    const pc = pcRef.current;
    if (!pc) return;
    const next = !muted;
    setMuted(next);
    pc.getSenders().forEach((s) => {
      if (s.track?.kind === "audio") s.track.enabled = !next;
    });
  }

  function toggleVideo() {
    const pc = pcRef.current;
    if (!pc) return;
    const next = !videoOff;
    setVideoOff(next);
    pc.getSenders().forEach((s) => {
      if (s.track?.kind === "video") s.track.enabled = !next;
    });
  }

  // MCU UX: один большой <video> со slot-0 (микшированный видео-поток с сервера) на весь экран,
  // local camera показываем маленьким PiP в углу — без задержки.
  // Layout редактор управляет ВНУТРЕННОСТЬЮ MCU-микса (через server compositor),
  // а не CSS-расположением клиентских <video>.
  //
  // Audio: server делает mix-minus через SFU-forward. Peer A's audio попадает в slot-A
  // у peer B,C,D (но НЕ у peer A → no self-echo). На клиенте каждый slot-N (N>0 в основном)
  // содержит audio track другого peer'а — играем их через скрытые <audio> элементы.
  const mcuStream = slotStreams["slot-0"] ?? null;
  const audioSlots = useMemo(
    () => Object.entries(slotStreams).filter(([, s]) => s.getAudioTracks().length > 0),
    [slotStreams],
  );

  const mcuRef = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    if (mcuRef.current && mcuStream && mcuRef.current.srcObject !== mcuStream) {
      mcuRef.current.srcObject = mcuStream;
    }
  }, [mcuStream]);

  return (
    <div className="h-full grid grid-rows-[1fr_4rem] bg-black text-white">
      <div className="relative bg-black">
        {/* MCU big stream */}
        <video
          ref={mcuRef}
          autoPlay
          playsInline
          className="absolute inset-0 w-full h-full object-contain bg-black"
        />
        {/* Self preview: маленький PiP в нижнем-левом углу */}
        <video
          ref={localRef}
          autoPlay
          playsInline
          muted
          className="absolute bottom-4 left-4 w-48 h-28 rounded-lg border-2 border-white/40 object-cover bg-slate-800 shadow-lg z-20"
        />
        {!mcuStream && state === "connected" && (
          <div className="absolute inset-0 grid place-items-center text-white/60 text-sm">
            Ожидание потока MCU…
          </div>
        )}
        {/* Скрытые <audio> для SFU-аудио от других peer-ов (mix-minus от сервера).
            Без них audio tracks из slot-N не воспроизводятся — большой <video> играет
            только slot-0, который теперь silent (сервер шлёт SFU audio в slot-N отправителя). */}
        {audioSlots.map(([slotId, s]) => (
          <AudioPlayer key={slotId} stream={s} />
        ))}
        <div className="absolute top-4 left-4 text-sm bg-black/50 rounded px-2 py-1 z-10">
          {room?.name ? <span className="font-medium">{room.name}</span> : <span className="font-mono">{id.slice(0, 8)}</span>}
          {" · "}{reconnecting ? "reconnecting…" : state}
          <span className="ml-2 inline-block bg-brand-700/70 px-2 py-0.5 rounded text-xs">
            layout: {layout.mode}
          </span>
          {reconnectCountRef.current > 0 && (
            <span className="ml-2 inline-block bg-yellow-600/80 px-2 py-0.5 rounded text-xs">
              reconnects: {reconnectCountRef.current}
            </span>
          )}
        </div>
        {state === "init" && (
          <button onClick={start} className="btn-primary absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 z-10">
            Подключиться (camera + mic)
          </button>
        )}
        {error && (
          <div className="absolute top-16 left-4 max-w-md text-sm bg-red-600/80 rounded px-3 py-2 z-10">
            {error}
          </div>
        )}
      </div>

      <div className="flex items-center justify-center gap-3 bg-slate-900">
        <button className="btn-ghost" title={muted ? "Unmute" : "Mute"} onClick={toggleMute} disabled={state !== "connected"}>
          {muted ? <MicOff className="size-5 text-red-500" /> : <Mic className="size-5" />}
        </button>
        <button className="btn-ghost" title={videoOff ? "Video on" : "Video off"} onClick={toggleVideo} disabled={state !== "connected"}>
          {videoOff ? <VideoOff className="size-5 text-red-500" /> : <VideoIcon className="size-5" />}
        </button>
        <button className="btn-danger" onClick={leave}>
          <PhoneOff className="size-5" /> Leave
        </button>
      </div>
    </div>
  );
}

function AudioPlayer({ stream }: { stream: MediaStream }) {
  const ref = useRef<HTMLAudioElement>(null);
  useEffect(() => {
    if (ref.current && ref.current.srcObject !== stream) {
      ref.current.srcObject = stream;
    }
  }, [stream]);
  return <audio ref={ref} autoPlay playsInline className="hidden" />;
}
