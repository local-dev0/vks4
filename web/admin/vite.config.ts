import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    proxy: {
      "/api":     { target: "http://control-plane:8080", changeOrigin: true },
      "/ws":      { target: "ws://signaling:8081",       ws: true, changeOrigin: true },
      "/grafana": { target: "http://grafana:3000",       changeOrigin: true },
      "/loki":    { target: "http://loki:3100",          changeOrigin: true },
    },
  },
  build: {
    sourcemap: true,
    target: "es2022",
  },
});
