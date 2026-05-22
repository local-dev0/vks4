import { api } from "@/lib/api";

export interface Room {
  id: string;
  name: string;
  description?: string;
  mode: "meeting" | "lecture" | "webinar";
  maxParticipants: number;
  waitingRoom: boolean;
  locked: boolean;
  recording: boolean;
  defaultLayout: Layout;
  joinUrl?: string;
  createdAt: string;
}

export interface Layout {
  mode: "grid" | "active_speaker" | "lecture" | "pip" | "custom";
  width?: number;
  height?: number;
  background?: { color?: string; url?: string };
  logo?: { url?: string; x?: number; y?: number; w?: number; h?: number };
  cells?: Array<{ id: string; x: number; y: number; w: number; h: number; zIndex?: number; vad?: boolean }>;
  showNames?: boolean;
  nameBgAlpha?: number;     // 0..1
  nameFontSize?: number;    // pt
  nameFontColor?: string;   // "#RRGGBB"
}

export interface Participant {
  peerId: string;
  userId?: string;
  displayName: string;
  role: "host" | "presenter" | "attendee";
  muted: boolean;
  videoOff: boolean;
  joinedAt: string;
  connection?: { transport: string; rtt?: number; jitter?: number; loss?: number; bitrate?: number };
}

export interface RoomsResp { items: Room[]; total: number }

export const RoomsApi = {
  list: (q?: string) => api<RoomsResp>(`/rooms${q ? `?q=${encodeURIComponent(q)}` : ""}`),
  get: (id: string) => api<Room>(`/rooms/${id}`),
  create: (body: Partial<Room>) => api<Room>(`/rooms`, { method: "POST", json: body }),
  update: (id: string, body: Partial<Room>) => api<Room>(`/rooms/${id}`, { method: "PATCH", json: body }),
  remove: (id: string) => api<void>(`/rooms/${id}`, { method: "DELETE" }),
  lock:   (id: string) => api<void>(`/rooms/${id}/lock`,   { method: "POST" }),
  unlock: (id: string) => api<void>(`/rooms/${id}/unlock`, { method: "POST" }),
  layout: (id: string, layout: Layout) => api<Layout>(`/rooms/${id}/layout`, { method: "PUT", json: layout }),
  // slots — body { "0": "peerId", "1": "peerId", ... } — оператор задаёт slot→peerID mapping
  // комнаты. После применения сервер пересобирает MCU compositor с новыми peer↔frame links.
  slots: (id: string, assign: Record<string, string>) =>
    api<void>(`/rooms/${id}/slots`, { method: "PUT", json: assign }),
  // getSlots — текущий slot→peerID mapping комнаты с сервера. Используется Room Control'ом
  // для инициализации state после перезагрузки страницы (иначе assignments были бы пусты).
  getSlots: (id: string) => api<Record<string, string>>(`/rooms/${id}/slots`),
  participants: (id: string) => api<Participant[]>(`/rooms/${id}/participants`),
  kick: (id: string, peerId: string) => api<void>(`/rooms/${id}/participants/${peerId}`, { method: "DELETE" }),
  startRecording: (id: string) => api(`/rooms/${id}/recording`, { method: "POST" }),
  stopRecording:  (id: string) => api(`/rooms/${id}/recording`, { method: "DELETE" }),
};
