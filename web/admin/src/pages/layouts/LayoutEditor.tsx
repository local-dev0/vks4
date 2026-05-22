import { useEffect, useRef, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
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
import { Trash2, LayoutGrid, ChevronDown, ChevronRight, Grid3x3, Save, Mic, MicOff } from "lucide-react";
import { type Layout } from "@/features/rooms/api";
import { LayoutsApi } from "@/features/layouts/api";

type Cell = NonNullable<Layout["cells"]>[number];

// Layout Editor — это **редактор шаблона раскладки** комнаты. Здесь оператор задаёт
// геометрию ячеек (frame-ов) со slot-N placeholder-ами. На странице Room Control
// затем оператор drag-drop'ом распределяет реальных peer-ов по этим slot'ам.
const SLOT_COUNT = 20;
const SLOTS: Array<{ id: string; label: string }> = Array.from({ length: SLOT_COUNT }, (_, i) => ({
  id: `slot-${i}`,
  label: `Slot ${i}`,
}));

// Палитра для слотов по индексу. Циклится по 8 цветам.
const SLOT_PALETTE = [
  "bg-blue-600", "bg-emerald-600", "bg-purple-600", "bg-amber-600",
  "bg-rose-600", "bg-cyan-600", "bg-fuchsia-600", "bg-lime-600",
];
function slotColor(id: string): string {
  if (id.startsWith("slot-")) {
    const n = parseInt(id.slice(5), 10);
    if (!isNaN(n)) return SLOT_PALETTE[n % SLOT_PALETTE.length];
  }
  return "bg-slate-600";
}

// LayoutEditor редактирует глобальный шаблон (LayoutTemplate). URL: /layouts/:id/edit.
// Привязка к комнате делается в Room Control — там оператор выбирает шаблон и применяет
// его cells к default_layout комнаты.
export function LayoutEditor() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const qc = useQueryClient();
  const tpl = useQuery({
    queryKey: ["layout-template", id],
    queryFn: () => LayoutsApi.get(id),
    enabled: !!id,
  });

  const [layout, setLayout] = useState<Layout>({
    mode: "custom",
    width: 1280,
    height: 720,
    cells: [],
  });
  const [name, setName] = useState("");
  const [settingsOpen, setSettingsOpen] = useState(false);
  // Стиль подписи peer-ов хранится в шаблоне. Локально храним в state, чтобы менять без
  // сетевых запросов до клика "Сохранить".
  const [nameStyle, setNameStyle] = useState({
    bgAlpha: 0.6,
    bgColor: "#000000",
    fontSize: 14,
    fontColor: "#FFFFFF",
  });

  useEffect(() => {
    if (tpl.data) {
      setLayout({
        mode: "custom",
        width: tpl.data.width,
        height: tpl.data.height,
        background: tpl.data.background,
        cells: tpl.data.cells,
      });
      setName(tpl.data.name);
      setNameStyle({
        bgAlpha: tpl.data.nameBgAlpha ?? 0.6,
        bgColor: tpl.data.nameBgColor ?? "#000000",
        fontSize: tpl.data.nameFontSize ?? 14,
        fontColor: tpl.data.nameFontColor ?? "#FFFFFF",
      });
    }
  }, [tpl.data]);

  const save = useMutation({
    mutationFn: () =>
      LayoutsApi.update(id, {
        name: name.trim() || tpl.data?.name || "Untitled",
        width: layout.width ?? 1280,
        height: layout.height ?? 720,
        cells: layout.cells ?? [],
        background: layout.background,
        nameBgAlpha: nameStyle.bgAlpha,
        nameBgColor: nameStyle.bgColor,
        nameFontSize: nameStyle.fontSize,
        nameFontColor: nameStyle.fontColor,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["layout-template", id] });
      qc.invalidateQueries({ queryKey: ["layout-templates"] });
    },
  });

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
  const canvasRef = useRef<HTMLDivElement>(null);

  function onDragEnd(e: DragEndEvent) {
    const dragId = String(e.active.id);
    const overId = e.over?.id;
    const W = layout.width ?? 1280;
    const H = layout.height ?? 720;
    const r = canvasRef.current?.getBoundingClientRect();
    const sx = r ? W / r.width : 1;
    const sy = r ? H / r.height : 1;

    if (dragId.startsWith("slot:")) {
      // Drop slot из sidebar → создаём frame с этим slot-id
      if (overId !== "canvas") return;
      const cellId = dragId.slice("slot:".length);
      const existing = (layout.cells ?? []).find((c) => c.id === cellId);
      if (existing) return; // уже размещён
      const defaultW = Math.round(W / 2);
      const defaultH = Math.round(H / 2);
      setLayout((p) => ({
        ...p,
        mode: "custom",
        cells: [
          ...(p.cells ?? []),
          { id: cellId, x: (W - defaultW) / 2, y: (H - defaultH) / 2, w: defaultW, h: defaultH },
        ],
      }));
      return;
    }

    if (dragId.startsWith("cell:")) {
      const cellId = dragId.slice("cell:".length);
      setLayout((p) => ({
        ...p,
        cells: (p.cells ?? []).map((c) =>
          c.id === cellId
            ? {
                ...c,
                x: Math.max(0, Math.min(W - c.w, c.x + e.delta.x * sx)),
                y: Math.max(0, Math.min(H - c.h, c.y + e.delta.y * sy)),
              }
            : c,
        ),
      }));
    }
  }

  function deleteCell(cellId: string) {
    setLayout((p) => ({ ...p, cells: (p.cells ?? []).filter((c) => c.id !== cellId) }));
  }
  function updateCell(cellId: string, w: number, h: number) {
    setLayout((p) => ({
      ...p,
      cells: (p.cells ?? []).map((c) => (c.id === cellId ? { ...c, w, h } : c)),
    }));
  }

  // Дефолтный пресет Grid 2×2 — всегда доступен как в-памяти заготовка.
  function applyDefaultGrid() {
    const W = layout.width || 1280;
    const H = layout.height || 720;
    const cells: Cell[] = [
      { id: "slot-0", x: 0, y: 0, w: W / 2, h: H / 2 },
      { id: "slot-1", x: W / 2, y: 0, w: W / 2, h: H / 2 },
      { id: "slot-2", x: 0, y: H / 2, w: W / 2, h: H / 2 },
      { id: "slot-3", x: W / 2, y: H / 2, w: W / 2, h: H / 2 },
    ];
    setLayout((p) => ({ ...p, mode: "custom", cells }));
  }

  const usedSlots = new Set((layout.cells ?? []).map((c) => c.id));

  return (
    <DndContext sensors={sensors} onDragEnd={onDragEnd}>
      <div className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <div className="flex-1 min-w-0">
            <input
              className="input text-2xl font-semibold tracking-tight bg-transparent border-0 px-0 focus:ring-0 w-full"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Название шаблона"
            />
            <div className="text-xs text-slate-500">
              Глобальный шаблон раскладки. Применяется к комнатам на странице{" "}
              <strong>Room Control</strong>.
            </div>
          </div>
          <div className="flex gap-2 shrink-0">
            <button className="btn-ghost" onClick={() => nav("/layouts")}>Назад к списку</button>
            <button
              className="btn-primary"
              onClick={() => save.mutate()}
              disabled={save.isPending}
            >
              {save.isPending ? "Сохраняем…" : save.isSuccess ? "Сохранено ✓" : <><Save className="size-4" /> Сохранить</>}
            </button>
          </div>
        </div>

        <div className="grid grid-cols-[14rem_1fr] gap-4">
          <aside className="space-y-3 max-h-[calc(100vh-9rem)] overflow-auto pr-1">
            <section className="card p-3 space-y-2">
              <div className="text-xs font-medium uppercase text-slate-500 flex items-center gap-1">
                <Grid3x3 className="size-3" /> Источники
              </div>
              <div className="text-[10px] text-slate-500 italic mb-1">
                Перетащите slot на холст. Каждая ячейка может быть отмечена флагом VAD —
                тогда она автоматически переключается на активного спикера.
              </div>
              <div className="grid grid-cols-4 gap-1 max-h-72 overflow-auto">
                {SLOTS.map((s) => (
                  <SlotCard key={s.id} id={s.id} label={s.label} used={usedSlots.has(s.id)} />
                ))}
              </div>
            </section>

            <section className="card p-3 space-y-2">
              <div className="text-xs font-medium uppercase text-slate-500">Быстрая заготовка</div>
              <button className="btn-ghost text-[11px] w-full" onClick={applyDefaultGrid}>
                <LayoutGrid className="size-3" /> Заполнить Grid 2×2
              </button>
              <button
                className="btn-ghost text-[11px] w-full text-red-500"
                onClick={() => setLayout((p) => ({ ...p, cells: [] }))}
              >
                <Trash2 className="size-3" /> Очистить холст
              </button>
            </section>

            <section className="card p-3 space-y-2">
              <button
                className="text-xs font-medium uppercase text-slate-500 flex items-center gap-1 w-full"
                onClick={() => setSettingsOpen((v) => !v)}
              >
                {settingsOpen ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
                Параметры холста
              </button>
              {settingsOpen && (
                <div className="space-y-2 pt-1">
                  <div className="grid grid-cols-2 gap-2">
                    <label className="block text-xs">
                      Width
                      <input className="input mt-1 text-xs" type="number" value={layout.width}
                        onChange={(e) => setLayout({ ...layout, width: +e.target.value })} />
                    </label>
                    <label className="block text-xs">
                      Height
                      <input className="input mt-1 text-xs" type="number" value={layout.height}
                        onChange={(e) => setLayout({ ...layout, height: +e.target.value })} />
                    </label>
                  </div>
                  <label className="block text-xs">
                    Фон
                    <input className="input mt-1 h-8 p-1" type="color"
                      value={layout.background?.color ?? "#000000"}
                      onChange={(e) => setLayout({ ...layout, background: { ...layout.background, color: e.target.value } })} />
                  </label>
                </div>
              )}
            </section>

            <section className="card p-3 space-y-2">
              <div className="text-xs font-medium uppercase text-slate-500">Стиль подписи</div>
              <label className="block text-xs space-y-1">
                <div className="flex justify-between">
                  <span>Прозрачность фона</span>
                  <span className="text-slate-400 font-mono">{Math.round(nameStyle.bgAlpha * 100)}%</span>
                </div>
                <input type="range" min={0} max={1} step={0.05}
                  value={nameStyle.bgAlpha}
                  onChange={(e) => setNameStyle((p) => ({ ...p, bgAlpha: +e.target.value }))}
                  className="w-full" />
              </label>
              <div className="grid grid-cols-2 gap-2">
                <label className="block text-xs space-y-1">
                  <div>Цвет фона</div>
                  <input type="color" value={nameStyle.bgColor}
                    onChange={(e) => setNameStyle((p) => ({ ...p, bgColor: e.target.value.toUpperCase() }))}
                    className="w-full h-8" />
                </label>
                <label className="block text-xs space-y-1">
                  <div>Цвет текста</div>
                  <input type="color" value={nameStyle.fontColor}
                    onChange={(e) => setNameStyle((p) => ({ ...p, fontColor: e.target.value.toUpperCase() }))}
                    className="w-full h-8" />
                </label>
              </div>
              <label className="block text-xs space-y-1">
                <div className="flex justify-between">
                  <span>Размер шрифта</span>
                  <span className="text-slate-400 font-mono">{nameStyle.fontSize} pt</span>
                </div>
                <input type="range" min={8} max={32} step={1}
                  value={nameStyle.fontSize}
                  onChange={(e) => setNameStyle((p) => ({ ...p, fontSize: +e.target.value }))}
                  className="w-full" />
              </label>
              <div className="text-[10px] text-slate-500 italic">
                Применяется при нажатии «Сохранить» вверху страницы.
              </div>
            </section>
          </aside>

          <DroppableCanvas
            canvasRef={canvasRef}
            layout={layout}
            onDelete={deleteCell}
            onResize={updateCell}
            onToggleVAD={(cellId) =>
              setLayout((p) => ({
                ...p,
                cells: (p.cells ?? []).map((c) => (c.id === cellId ? { ...c, vad: !c.vad } : c)),
              }))
            }
          />
        </div>
      </div>
    </DndContext>
  );
}

function SlotCard({ id, label, used }: { id: string; label: string; used: boolean }) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({ id: `slot:${id}` });
  return (
    <div
      ref={setNodeRef}
      style={{
        transform: transform ? `translate3d(${transform.x}px,${transform.y}px,0)` : undefined,
        opacity: isDragging ? 0.5 : 1,
      }}
      {...listeners}
      {...attributes}
      className={`relative cursor-grab active:cursor-grabbing rounded p-2 text-white text-center select-none ${slotColor(id)} ${used ? "ring-2 ring-emerald-400" : ""}`}
    >
      <div className="text-base font-bold leading-none">{id.slice(5)}</div>
      <div className="text-[9px] opacity-80 leading-tight mt-0.5">{label}</div>
      {used && (
        <div className="absolute -top-1 -right-1 bg-emerald-500 text-white text-[9px] rounded-full px-1.5 py-0.5">
          ✓
        </div>
      )}
    </div>
  );
}

function DroppableCanvas(props: {
  canvasRef: React.RefObject<HTMLDivElement | null>;
  layout: Layout;
  onDelete: (id: string) => void;
  onResize: (id: string, w: number, h: number) => void;
  onToggleVAD: (id: string) => void;
}) {
  const { canvasRef, layout, onDelete, onResize, onToggleVAD } = props;
  const { setNodeRef, isOver } = useDroppable({ id: "canvas" });
  const W = layout.width ?? 1280;
  const H = layout.height ?? 720;
  return (
    <div className="grid place-items-start">
      <div
        ref={(node) => {
          setNodeRef(node);
          if (canvasRef && node) (canvasRef as React.MutableRefObject<HTMLDivElement | null>).current = node;
        }}
        className={`relative overflow-hidden rounded-lg shadow-xl ${isOver ? "ring-2 ring-brand-400" : ""}`}
        style={{
          width: "100%",
          maxWidth: W,
          aspectRatio: `${W} / ${H}`,
          background: layout.background?.color ?? "#0b1220",
        }}
      >
        {(layout.cells ?? []).map((c) => (
          <CanvasCell
            key={c.id}
            cell={c}
            canvasW={W}
            canvasH={H}
            onDelete={() => onDelete(c.id)}
            onResize={(w, h) => onResize(c.id, w, h)}
            onToggleVAD={() => onToggleVAD(c.id)}
          />
        ))}
        {(layout.cells ?? []).length === 0 && (
          <div className="absolute inset-0 grid place-items-center pointer-events-none">
            <div className="text-slate-400 text-sm text-center">
              <Grid3x3 className="size-8 mx-auto mb-2 opacity-50" />
              Перетащите slot сюда<br />
              <span className="text-xs">или примените preset слева</span>
            </div>
          </div>
        )}
      </div>
      <div className="mt-2 text-xs text-slate-500">
        Canvas <span className="font-mono">{W}×{H}</span> · drag = move · угол справа-снизу = resize
      </div>
    </div>
  );
}

function CanvasCell(props: {
  cell: Cell;
  canvasW: number;
  canvasH: number;
  onDelete: () => void;
  onResize: (w: number, h: number) => void;
  onToggleVAD: () => void;
}) {
  const { cell, canvasW, canvasH, onDelete, onResize, onToggleVAD } = props;
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({ id: `cell:${cell.id}` });

  function onResizeMouseDown(e: React.MouseEvent) {
    e.stopPropagation();
    e.preventDefault();
    const startX = e.clientX;
    const startY = e.clientY;
    const startW = cell.w;
    const startH = cell.h;
    const parent = (e.currentTarget as HTMLElement).closest(".relative")?.getBoundingClientRect();
    const sx = parent ? canvasW / parent.width : 1;
    const sy = parent ? canvasH / parent.height : 1;
    function onMove(ev: MouseEvent) {
      const newW = Math.max(80, Math.min(canvasW - cell.x, startW + (ev.clientX - startX) * sx));
      const newH = Math.max(60, Math.min(canvasH - cell.y, startH + (ev.clientY - startY) * sy));
      onResize(newW, newH);
    }
    function onUp() {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    }
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }

  const tx = transform ? transform.x : 0;
  const ty = transform ? transform.y : 0;

  return (
    <div
      ref={setNodeRef}
      className={`absolute group select-none cursor-grab active:cursor-grabbing rounded shadow-md ${slotColor(cell.id)} ${isDragging ? "opacity-60" : ""}`}
      style={{
        left: `${(cell.x / canvasW) * 100}%`,
        top: `${(cell.y / canvasH) * 100}%`,
        width: `${(cell.w / canvasW) * 100}%`,
        height: `${(cell.h / canvasH) * 100}%`,
        zIndex: cell.zIndex ?? 0,
        transform: `translate3d(${tx}px,${ty}px,0)`,
      }}
      {...listeners}
      {...attributes}
    >
      <div className="absolute inset-0 grid place-items-center pointer-events-none">
        <div className="text-white text-center">
          <div className="text-3xl font-bold opacity-90">{cell.id.slice(5)}</div>
          <div className="text-xs opacity-75 mt-1">{cell.id}</div>
          {cell.vad && (
            <div className="text-[10px] opacity-90 mt-1 bg-orange-500/80 px-1.5 py-0.5 rounded inline-flex items-center gap-0.5">
              <Mic className="size-2.5" /> VAD
            </div>
          )}
        </div>
      </div>
      <div className="absolute top-1 right-1 flex gap-1 opacity-0 group-hover:opacity-100 transition">
        <button
          className={`text-white rounded p-1 ${cell.vad ? "bg-orange-500 hover:bg-orange-600" : "bg-black/40 hover:bg-orange-500"}`}
          onPointerDown={(e) => e.stopPropagation()}
          onClick={onToggleVAD}
          title={cell.vad ? "Отключить VAD-переключение" : "Включить VAD-переключение"}
        >
          {cell.vad ? <Mic className="size-3" /> : <MicOff className="size-3" />}
        </button>
        <button
          className="bg-black/40 hover:bg-red-500 text-white rounded p-1"
          onPointerDown={(e) => e.stopPropagation()}
          onClick={onDelete}
          title="Удалить"
        >
          <Trash2 className="size-3" />
        </button>
      </div>
      <div className="absolute bottom-1 left-1 text-[9px] text-white/70 font-mono bg-black/40 px-1 rounded">
        {Math.round(cell.w)}×{Math.round(cell.h)}
      </div>
      <div
        onMouseDown={onResizeMouseDown}
        onPointerDown={(e) => e.stopPropagation()}
        className="absolute right-0 bottom-0 w-3 h-3 bg-white/80 cursor-se-resize"
        style={{ touchAction: "none" }}
        title="Resize"
      />
    </div>
  );
}
