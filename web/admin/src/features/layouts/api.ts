import { api } from "@/lib/api";
import type { Layout } from "@/features/rooms/api";

// LayoutTemplate — переиспользуемый шаблон раскладки. Создаётся в LayoutEditor через
// "Save as template", применяется к комнатам как заготовка cells (со slot-N placeholder'ами).
export interface LayoutTemplate {
  id: string;
  name: string;
  width: number;
  height: number;
  cells: NonNullable<Layout["cells"]>;
  background?: { color?: string; url?: string };
  nameBgAlpha?: number;
  nameBgColor?: string;
  nameFontSize?: number;
  nameFontColor?: string;
  createdBy?: string;
  createdAt: string;
}

export interface LayoutTemplateInput {
  name: string;
  width: number;
  height: number;
  cells: NonNullable<Layout["cells"]>;
  background?: { color?: string; url?: string };
  nameBgAlpha?: number;
  nameBgColor?: string;
  nameFontSize?: number;
  nameFontColor?: string;
}

export const LayoutsApi = {
  list: () => api<{ items: LayoutTemplate[] }>(`/layout-templates`),
  get: (id: string) => api<LayoutTemplate>(`/layout-templates/${id}`),
  create: (body: LayoutTemplateInput) =>
    api<LayoutTemplate>(`/layout-templates`, { method: "POST", json: body }),
  update: (id: string, body: Partial<LayoutTemplateInput>) =>
    api<LayoutTemplate>(`/layout-templates/${id}`, { method: "PATCH", json: body }),
  remove: (id: string) => api<void>(`/layout-templates/${id}`, { method: "DELETE" }),
};
