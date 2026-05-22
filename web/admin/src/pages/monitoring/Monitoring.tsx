import { useQuery } from "@tanstack/react-query";
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts";
import { api } from "@/lib/api";

interface SystemHealth {
  nodes: Array<{ id: string; role: string; cpu?: number; memory?: number; status: string }>;
  rooms: number;
  participants: number;
  alerts: Array<{ level: string; message: string }>;
}

export function Monitoring() {
  const health = useQuery({
    queryKey: ["system-health"],
    queryFn: () => api<SystemHealth>("/monitoring/system"),
    refetchInterval: 5000,
  });

  // Демо-данные для графика. В prod подключите Prometheus query proxy через control-plane.
  const series = Array.from({ length: 30 }, (_, i) => ({
    t: i,
    bitrate: 1500 + Math.round(Math.sin(i / 3) * 500 + Math.random() * 200),
    loss: Math.max(0, Math.round(Math.sin(i / 5) * 2 + Math.random() * 1)),
  }));

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold tracking-tight">Monitoring</h1>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div className="card p-4">
          <div className="text-sm text-slate-500">Active rooms</div>
          <div className="text-2xl font-semibold">{health.data?.rooms ?? 0}</div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-slate-500">Active participants</div>
          <div className="text-2xl font-semibold">{health.data?.participants ?? 0}</div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-slate-500">Alerts</div>
          <div className="text-2xl font-semibold">{health.data?.alerts?.length ?? 0}</div>
        </div>
      </div>

      <div className="card p-4">
        <div className="text-sm text-slate-500 mb-2">Aggregate bitrate (kbps)</div>
        <div style={{ width: "100%", height: 240 }}>
          <ResponsiveContainer>
            <LineChart data={series}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="t" />
              <YAxis />
              <Tooltip />
              <Line type="monotone" dataKey="bitrate" stroke="#6366f1" dot={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>
      </div>

      <div className="card p-4">
        <div className="text-sm text-slate-500 mb-2">Packet loss (%)</div>
        <div style={{ width: "100%", height: 240 }}>
          <ResponsiveContainer>
            <LineChart data={series}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="t" />
              <YAxis />
              <Tooltip />
              <Line type="monotone" dataKey="loss" stroke="#ef4444" dot={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>
      </div>

      <div className="card p-4">
        <div className="text-sm text-slate-500 mb-2">Embedded Grafana</div>
        <iframe src="/grafana/d/vks4-rooms-qos/vks4-rooms-qos?orgId=1&kiosk" className="w-full h-[480px] rounded border" />
      </div>
    </div>
  );
}
