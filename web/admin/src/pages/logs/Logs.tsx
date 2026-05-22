import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

interface LokiResp {
  data?: {
    result: Array<{ stream: Record<string, string>; values: [string, string][] }>;
  };
}

export function Logs() {
  const [q, setQ] = useState('{service=~".+"}');
  const logs = useQuery({
    queryKey: ["logs", q],
    queryFn: async () => {
      // Proxy через Caddy; в prod закройте за RBAC в control-plane.
      const url = `/loki/loki/api/v1/query_range?query=${encodeURIComponent(q)}&limit=200`;
      const r = await fetch(url);
      if (!r.ok) throw new Error("loki failed");
      return (await r.json()) as LokiResp;
    },
    refetchInterval: 10_000,
  });

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold tracking-tight">Logs</h1>
      <input className="input" value={q} onChange={(e) => setQ(e.target.value)} placeholder="LogQL query" />
      <div className="card p-2 max-h-[70vh] overflow-auto">
        <pre className="text-xs whitespace-pre-wrap font-mono">
          {logs.data?.data?.result?.flatMap((s) =>
            s.values.map(([ts, line]) => `${new Date(+ts / 1_000_000).toISOString()} [${s.stream.service ?? "?"}] ${line}`),
          ).slice(-200).join("\n") ?? "Loading…"}
        </pre>
      </div>
    </div>
  );
}
