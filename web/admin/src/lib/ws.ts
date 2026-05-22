// Тонкая обёртка для WS-сигналинга.
// Подключение: const ws = createSignalingClient(token, roomId)
export interface Envelope<T = unknown> {
  type: string;
  id?: string;
  payload?: T;
}

export type Listener = (env: Envelope) => void;

export interface SignalingClient {
  send: (env: Envelope) => void;
  on: (type: string, fn: Listener) => () => void;
  close: () => void;
}

export function createSignalingClient(token: string, roomId: string, displayName: string): SignalingClient {
  const url = (location.protocol === "https:" ? "wss://" : "ws://") + location.host + "/ws";
  const ws = new WebSocket(url, "vks4.signaling.v1");
  const listeners = new Map<string, Set<Listener>>();
  let ready = false;
  const queue: Envelope[] = [];

  ws.addEventListener("open", () => {
    ready = true;
    ws.send(JSON.stringify({ type: "join", payload: { token, roomId, displayName } }));
    queue.forEach((q) => ws.send(JSON.stringify(q)));
    queue.length = 0;
  });
  ws.addEventListener("message", (e) => {
    const env = JSON.parse(e.data) as Envelope;
    listeners.get(env.type)?.forEach((fn) => fn(env));
  });

  return {
    send(env) {
      if (ready) ws.send(JSON.stringify(env));
      else queue.push(env);
    },
    on(type, fn) {
      let set = listeners.get(type);
      if (!set) { set = new Set(); listeners.set(type, set); }
      set.add(fn);
      return () => set!.delete(fn);
    },
    close() { ws.close(1000, "bye"); },
  };
}
