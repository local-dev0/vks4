import { useEffect, useMemo, useState } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DndContext,
  type DragEndEvent,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { Users, LayoutGrid, Sparkles, X, BookOpen, Mic, Eye, EyeOff } from "lucide-react";
import { RoomsApi, type Layout, type Participant } from "@/features/rooms/api";
import { LayoutsApi } from "@/features/layouts/api";

// Room Control — экран оператора активной комнаты.
// На холсте показаны фреймы из defaultLayout.cells (определяются в Layout Editor).
// Sidebar — roster (реально подключенные peer'ы). Перетягиваешь peer'а на фрейм →
// сервер делает PUT /rooms/{id}/slots {N: peerID}, MCU compositor сразу применяет.

const SLOT_PALETTE = [
  "bg-blue-600", "bg-emerald-600", "bg-purple-600", "bg-amber-600",
  "bg-rose-600", "bg-cyan-600", "bg-fuchsia-600", "bg-lime-600",
];
function slotBg(id: string): string {
  if (id === "speaker") return "bg-orange-500";
  if (id.startsWith("slot-")) {
    const n = parseInt(id.slice(5), 10);
    if (!isNaN(n)) return SLOT_PALETTE[n % SLOT_PALETTE.length];
  }
  return "bg-slate-600";
}

function initials(name: string): string {
  if (!name) return "?";
  const parts = name.split(/[\s@.]+/).filter(Boolean);
  if (parts.length === 0) return name.slice(0, 2).toUpperCase();
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

function colorFor(id: string): string {
  const palette = [
    "bg-blue-600", "bg-emerald-600", "bg-purple-600", "bg-amber-600",
    "bg-rose-600", "bg-cyan-600", "bg-fuchsia-600", "bg-lime-600",
  ];
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return palette[h % palette.length];
}

export function RoomControl() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const qc = useQueryClient();
  const room = useQuery({ queryKey: ["room", id], queryFn: () => RoomsApi.get(id), enabled: !!id });
  const participants = useQuery<Participant[]>({
    queryKey: ["room", id, "participants"],
    queryFn: () => RoomsApi.participants(id),
    enabled: !!id,
    refetchInterval: 2000,
  });
  // Текущий slot→peerID из media-worker, обновляется параллельно с participants.
  // Это источник правды для assignments — сохраняет state через reload страницы.
  const serverSlots = useQuery<Record<string, string>>({
    queryKey: ["room", id, "slots"],
    queryFn: () => RoomsApi.getSlots(id),
    enabled: !!id,
    refetchInterval: 2000,
  });

  // Локальный mapping slot→peerID. Инициализируется из serverSlots и обновляется при drag.
  // Если изменения не сохранены через Apply, локальный state живёт независимо.
  const [assignments, setAssignments] = useState<Record<string, string>>({});
  const [dirty, setDirty] = useState(false);
  // Когда serverSlots приходят и нет несохранённых изменений — синхронизируем локальный state.
  useEffect(() => {
    if (!dirty && serverSlots.data) {
      setAssignments(serverSlots.data);
    }
  }, [serverSlots.data, dirty]);

  const layout = room.data?.defaultLayout;
  const cells = useMemo(() => layout?.cells ?? [], [layout]);
  const roster = participants.data ?? [];

  // peerID который уже куда-то назначен — чтобы пометить карточку «used»
  const assignedPeers = useMemo(() => new Set(Object.values(assignments).filter(Boolean)), [assignments]);

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  const apply = useMutation({
    mutationFn: () => RoomsApi.slots(id, assignments),
    onSuccess: () => {
      setDirty(false);
      qc.invalidateQueries({ queryKey: ["room", id, "participants"] });
      qc.invalidateQueries({ queryKey: ["room", id, "slots"] });
    },
  });

  // Глобальные шаблоны для применения к комнате.
  const templates = useQuery({
    queryKey: ["layout-templates"],
    queryFn: () => LayoutsApi.list(),
  });

  // Toggle подписей участников в MCU output.
  const showNames = room.data?.defaultLayout?.showNames !== false;
  const toggleNames = useMutation({
    mutationFn: () => {
      const cur = room.data?.defaultLayout ?? { mode: "custom" };
      return RoomsApi.layout(id, { ...cur, showNames: !showNames });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["room", id] }),
  });

  // Стиль подписи peer-ов.
  const styleBgAlpha = room.data?.defaultLayout?.nameBgAlpha ?? 0.6;
  const styleFontSize = room.data?.defaultLayout?.nameFontSize ?? 14;
  const styleFontColor = room.data?.defaultLayout?.nameFontColor ?? "#FFFFFF";
  const setStyle = useMutation({
    mutationFn: (patch: { nameBgAlpha?: number; nameFontSize?: number; nameFontColor?: string }) => {
      const cur = room.data?.defaultLayout ?? { mode: "custom" };
      return RoomsApi.layout(id, { ...cur, ...patch });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["room", id] }),
  });
  const applyTemplate = useMutation({
    mutationFn: async (tplId: string) => {
      const t = await LayoutsApi.get(tplId);
      // PATCH room.defaultLayout = { mode: "custom", ...t.cells }
      await RoomsApi.layout(id, {
        mode: "custom",
        width: t.width,
        height: t.height,
        background: t.background,
        cells: t.cells,
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["room", id] });
    },
  });

  function onDragEnd(e: DragEndEvent) {
    const dragId = String(e.active.id);
    const overId = e.over?.id;
    if (!dragId.startsWith("peer:") || !overId || !String(overId).startsWith("frame:")) return;
    const peerID = dragId.slice("peer:".length);
    const slotID = String(overId).slice("frame:".length); // "slot-0"
    setAssignments((prev) => {
      const next = { ...prev };
      // если этот peer уже где-то — освобождаем тот frame
      for (const [k, v] of Object.entries(next)) if (v === peerID) delete next[k];
      // slot — номер для сервера
      const slotN = slotID.startsWith("slot-") ? slotID.slice(5) : slotID;
      next[slotN] = peerID;
      return next;
    });
    setDirty(true);
  }

  function clearSlot(slotID: string) {
    const slotN = slotID.startsWith("slot-") ? slotID.slice(5) : slotID;
    setAssignments((prev) => {
      const next = { ...prev };
      next[slotN] = "";
      return next;
    });
    setDirty(true);
  }

  const hasLayout = cells.length > 0;
  const W = layout?.width ?? 1280;
  const H = layout?.height ?? 720;

  return (
    <DndContext sensors={sensors} onDragEnd={onDragEnd}>
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">
              {room.data?.name ?? "Room control"}
            </h1>
            <div className="text-xs text-slate-500">
              Перетащите участника на frame чтобы назначить его в этот слот MCU-микса.
            </div>
          </div>
          <div className="flex gap-2 items-center">
            <button
              className={`btn-ghost text-xs ${showNames ? "" : "opacity-60"}`}
              onClick={() => toggleNames.mutate()}
              disabled={toggleNames.isPending}
              title={showNames ? "Подписи: ON — клик чтобы выключить" : "Подписи: OFF — клик чтобы включить"}
            >
              {showNames ? <Eye className="size-4 text-emerald-500" /> : <EyeOff className="size-4 text-slate-400" />}
              {showNames ? "Подписи" : "Без подписей"}
            </button>
            <Link to="/layouts" className="btn-ghost text-xs">
              <BookOpen className="size-4" /> Templates
            </Link>
            <button className="btn-ghost" onClick={() => nav(`/rooms/${id}`)}>Назад</button>
            <button
              className="btn-primary"
              onClick={() => apply.mutate()}
              disabled={apply.isPending || !dirty}
            >
              {apply.isPending ? "Применяем…" : !dirty && apply.isSuccess ? "Применено ✓" : "Apply"}
            </button>
          </div>
        </div>

        <div className="grid grid-cols-[16rem_1fr] gap-4">
          <aside className="space-y-3 max-h-[calc(100vh-9rem)] overflow-auto pr-1">
            <section className="card p-3 space-y-2">
              <div className="text-xs font-medium uppercase text-slate-500 flex items-center gap-1">
                <Users className="size-3" /> В комнате ({roster.length})
              </div>
              {roster.length === 0 ? (
                <div className="text-[11px] text-slate-500 italic">
                  Никто не подключился. Зайдите в комнату двумя браузерами для теста.
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-2">
                  {roster.map((p) => (
                    <PeerCard
                      key={p.peerId}
                      peerId={p.peerId}
                      displayName={p.displayName}
                      used={assignedPeers.has(p.peerId)}
                    />
                  ))}
                </div>
              )}
            </section>

            <section className="card p-3 space-y-2">
              <div className="text-xs font-medium uppercase text-slate-500 flex items-center justify-between">
                <span className="flex items-center gap-1"><BookOpen className="size-3" /> Шаблоны</span>
                <Link to="/layouts" className="text-brand-600 hover:underline text-[11px]">Manage</Link>
              </div>
              {(templates.data?.items ?? []).length === 0 && (
                <div className="text-[11px] text-slate-500 italic">
                  Нет шаблонов. Создайте на странице <Link to="/layouts" className="text-brand-600">Layouts</Link>.
                </div>
              )}
              <div className="space-y-1 max-h-56 overflow-auto">
                {(templates.data?.items ?? []).map((t) => (
                  <button
                    key={t.id}
                    className="btn-ghost text-[11px] w-full justify-start"
                    onClick={() => {
                      if (confirm(`Применить шаблон «${t.name}»? Текущая раскладка комнаты будет заменена.`)) {
                        applyTemplate.mutate(t.id);
                      }
                    }}
                    disabled={applyTemplate.isPending}
                    title={`${t.name} · ${t.cells.length} cells · ${t.width}×${t.height}`}
                  >
                    <span className="truncate">{t.name}</span>
                    <span className="ml-auto text-slate-400 text-[10px]">{t.cells.length}</span>
                  </button>
                ))}
              </div>
            </section>

            {/* Стиль подписи peer-ов (textoverlay) */}
            <section className="card p-3 space-y-2">
              <div className="text-xs font-medium uppercase text-slate-500">Стиль подписи</div>
              <label className="block text-xs space-y-1">
                <div className="flex justify-between">
                  <span>Фон (затемнение)</span>
                  <span className="text-slate-400 font-mono">{Math.round(styleBgAlpha * 100)}%</span>
                </div>
                <input
                  type="range" min={0} max={1} step={0.05}
                  value={styleBgAlpha}
                  onChange={(e) => setStyle.mutate({ nameBgAlpha: +e.target.value })}
                  className="w-full"
                />
              </label>
              <label className="block text-xs space-y-1">
                <div className="flex justify-between">
                  <span>Размер шрифта</span>
                  <span className="text-slate-400 font-mono">{styleFontSize} pt</span>
                </div>
                <input
                  type="range" min={8} max={32} step={1}
                  value={styleFontSize}
                  onChange={(e) => setStyle.mutate({ nameFontSize: +e.target.value })}
                  className="w-full"
                />
              </label>
              <label className="block text-xs space-y-1">
                <div>Цвет текста</div>
                <input
                  type="color"
                  value={styleFontColor}
                  onChange={(e) => setStyle.mutate({ nameFontColor: e.target.value.toUpperCase() })}
                  className="w-full h-8"
                />
              </label>
            </section>

            <section className="card p-3 text-xs space-y-1">
              <div className="font-medium uppercase text-slate-500 flex items-center gap-1">
                <Sparkles className="size-3" /> Подсказка
              </div>
              <div className="text-slate-600 dark:text-slate-400 leading-relaxed">
                После <strong>Apply</strong> MCU-картинка во всех браузерах перестроится в течение ~1 сек.
              </div>
            </section>
          </aside>

          {/* canvas with frames */}
          <div className="grid place-items-start">
            {!hasLayout ? (
              <div className="card p-8 text-center w-full">
                <LayoutGrid className="size-10 mx-auto mb-3 text-slate-400" />
                <div className="text-sm text-slate-600 mb-3">
                  У этой комнаты нет настроенной раскладки.
                </div>
                <Link to={`/rooms/${id}/layout`} className="btn-primary">
                  Создать раскладку
                </Link>
              </div>
            ) : (
              <div
                className="relative overflow-hidden rounded-lg shadow-xl"
                style={{
                  width: "100%",
                  maxWidth: W,
                  aspectRatio: `${W} / ${H}`,
                  background: layout?.background?.color ?? "#0b1220",
                }}
              >
                {cells.map((c) => {
                  const slotN = c.id.startsWith("slot-") ? c.id.slice(5) : c.id;
                  const peerID = assignments[slotN];
                  const peer = peerID ? roster.find((p) => p.peerId === peerID) : null;
                  return (
                    <Frame
                      key={c.id}
                      cell={c}
                      canvasW={W}
                      canvasH={H}
                      peer={peer ?? null}
                      onClear={() => clearSlot(c.id)}
                    />
                  );
                })}
              </div>
            )}
            {hasLayout && (
              <div className="mt-2 text-xs text-slate-500">
                Canvas <span className="font-mono">{W}×{H}</span> · занято слотов:{" "}
                {Object.values(assignments).filter(Boolean).length}/{cells.length}
              </div>
            )}
          </div>
        </div>
      </div>
    </DndContext>
  );
}

function PeerCard(props: { peerId: string; displayName: string; used: boolean }) {
  const { peerId, displayName, used } = props;
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: `peer:${peerId}`,
  });
  return (
    <div
      ref={setNodeRef}
      style={{
        transform: transform ? `translate3d(${transform.x}px,${transform.y}px,0)` : undefined,
        opacity: isDragging ? 0.5 : 1,
      }}
      {...listeners}
      {...attributes}
      className={`relative cursor-grab active:cursor-grabbing rounded p-2 text-white text-center select-none ${colorFor(peerId)} ${used ? "ring-2 ring-emerald-400" : ""}`}
      title={`${displayName} (drag to a frame)`}
    >
      <div className="w-10 h-10 mx-auto mb-1 rounded-full bg-black/30 grid place-items-center text-sm font-bold">
        {initials(displayName)}
      </div>
      <div className="text-[11px] font-medium truncate">{displayName || "guest"}</div>
      <div className="text-[9px] opacity-70 font-mono truncate">{peerId.slice(0, 6)}</div>
      {used && (
        <div className="absolute -top-1 -right-1 bg-emerald-500 text-white text-[9px] rounded-full px-1.5 py-0.5">
          ✓
        </div>
      )}
    </div>
  );
}

function Frame(props: {
  cell: NonNullable<Layout["cells"]>[number];
  canvasW: number;
  canvasH: number;
  peer: Participant | null;
  onClear: () => void;
}) {
  const { cell, canvasW, canvasH, peer, onClear } = props;
  const { isOver, setNodeRef } = useDroppable({ id: `frame:${cell.id}` });
  return (
    <div
      ref={setNodeRef}
      className={`absolute rounded border-2 transition ${
        peer
          ? `${slotBg(cell.id)} border-transparent`
          : "border-dashed border-white/30"
      } ${isOver ? "ring-4 ring-emerald-400" : ""}`}
      style={{
        left: `${(cell.x / canvasW) * 100}%`,
        top: `${(cell.y / canvasH) * 100}%`,
        width: `${(cell.w / canvasW) * 100}%`,
        height: `${(cell.h / canvasH) * 100}%`,
        zIndex: cell.zIndex ?? 0,
        background: peer ? undefined : "rgba(255,255,255,0.05)",
      }}
    >
      <div className="absolute inset-0 grid place-items-center pointer-events-none">
        {peer ? (
          <div className="text-white text-center">
            <div className="text-3xl font-bold opacity-90">{initials(peer.displayName)}</div>
            <div className="text-sm opacity-90 mt-1 px-2 truncate">{peer.displayName || "guest"}</div>
            <div className="text-[10px] opacity-60 font-mono">{peer.peerId.slice(0, 6)}</div>
          </div>
        ) : (
          <div className="text-white/40 text-center">
            <div className="text-2xl font-bold">{cell.id.slice(5)}</div>
            <div className="text-xs opacity-70">{cell.id}</div>
            <div className="text-[10px] opacity-50 mt-1">drop peer here</div>
          </div>
        )}
      </div>
      {peer && (
        <button
          className="absolute top-1 right-1 bg-black/40 hover:bg-red-500 text-white rounded p-1"
          onClick={onClear}
          title="Освободить слот"
        >
          <X className="size-3" />
        </button>
      )}
      <div className="absolute bottom-1 left-1 text-[9px] text-white/70 font-mono bg-black/40 px-1 rounded flex items-center gap-1">
        {cell.id}
        {cell.vad && <Mic className="size-2.5 text-orange-300" />}
      </div>
    </div>
  );
}
