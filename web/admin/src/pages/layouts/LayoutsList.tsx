import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, Edit3, LayoutGrid as LayoutGridIcon } from "lucide-react";
import { LayoutsApi, type LayoutTemplate } from "@/features/layouts/api";

export function LayoutsList() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["layout-templates"], queryFn: () => LayoutsApi.list() });

  const create = useMutation({
    mutationFn: (name: string) =>
      LayoutsApi.create({
        name,
        width: 1280,
        height: 720,
        cells: [],
      }),
    onSuccess: (t) => {
      qc.invalidateQueries({ queryKey: ["layout-templates"] });
      nav(`/layouts/${t.id}/edit`);
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => LayoutsApi.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["layout-templates"] }),
  });

  function newTemplate() {
    const name = prompt("Название нового шаблона:");
    if (name && name.trim()) create.mutate(name.trim());
  }

  const items = list.data?.items ?? [];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Layout templates</h1>
          <p className="text-sm text-slate-500">
            Переиспользуемые раскладки. Создаются здесь, применяются к комнатам в Room Control.
          </p>
        </div>
        <button className="btn-primary" onClick={newTemplate} disabled={create.isPending}>
          <Plus className="size-4" /> Новый шаблон
        </button>
      </div>

      {list.isLoading && <div className="text-slate-500 text-sm">Загрузка…</div>}
      {items.length === 0 && !list.isLoading && (
        <div className="card p-8 text-center">
          <LayoutGridIcon className="size-10 mx-auto mb-3 text-slate-400" />
          <div className="text-sm text-slate-600 mb-3">Пока нет шаблонов.</div>
          <button className="btn-primary" onClick={newTemplate}>
            <Plus className="size-4" /> Создать первый
          </button>
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        {items.map((t: LayoutTemplate) => (
          <div key={t.id} className="card p-3 space-y-2">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0">
                <Link to={`/layouts/${t.id}/edit`} className="font-medium hover:text-brand-600 block truncate">
                  {t.name}
                </Link>
                <div className="text-xs text-slate-500 font-mono">
                  {t.width}×{t.height} · {t.cells.length} cells
                </div>
              </div>
              <div className="flex gap-1 shrink-0">
                <Link to={`/layouts/${t.id}/edit`} className="btn-ghost p-1.5" title="Редактировать">
                  <Edit3 className="size-3.5" />
                </Link>
                <button
                  className="btn-ghost p-1.5 text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20"
                  title="Удалить"
                  onClick={() => {
                    if (confirm(`Удалить шаблон «${t.name}»?`)) remove.mutate(t.id);
                  }}
                >
                  <Trash2 className="size-3.5" />
                </button>
              </div>
            </div>
            <LayoutThumb cells={t.cells} canvasW={t.width} canvasH={t.height} />
          </div>
        ))}
      </div>
    </div>
  );
}

const SLOT_PALETTE = [
  "bg-blue-500", "bg-emerald-500", "bg-purple-500", "bg-amber-500",
  "bg-rose-500", "bg-cyan-500", "bg-fuchsia-500", "bg-lime-500",
];
function slotColor(id: string): string {
  if (id === "speaker") return "bg-orange-500";
  if (id.startsWith("slot-")) {
    const n = parseInt(id.slice(5), 10);
    if (!isNaN(n)) return SLOT_PALETTE[n % SLOT_PALETTE.length];
  }
  return "bg-slate-500";
}

function LayoutThumb({ cells, canvasW, canvasH }: { cells: LayoutTemplate["cells"]; canvasW: number; canvasH: number }) {
  return (
    <div
      className="relative w-full rounded bg-slate-900 overflow-hidden"
      style={{ aspectRatio: `${canvasW} / ${canvasH}` }}
    >
      {cells.map((c) => (
        <div
          key={c.id}
          className={`absolute rounded text-white text-[8px] font-bold grid place-items-center ${slotColor(c.id)}`}
          style={{
            left: `${(c.x / canvasW) * 100}%`,
            top: `${(c.y / canvasH) * 100}%`,
            width: `${(c.w / canvasW) * 100}%`,
            height: `${(c.h / canvasH) * 100}%`,
          }}
        >
          {c.id === "speaker" ? "🎤" : c.id.startsWith("slot-") ? c.id.slice(5) : "?"}
        </div>
      ))}
    </div>
  );
}
