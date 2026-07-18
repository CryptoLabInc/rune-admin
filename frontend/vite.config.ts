/// <reference types="vitest/config" />
import { resolve } from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Dev proxy target for the BFF namespaces. Defaults to the loopback console
// listener (the real Go backend); set MOCK_TARGET=http://localhost:4000 to
// point the SPA at the standalone mock server (mock-server/) instead.
const proxyTarget = process.env.MOCK_TARGET ?? "http://127.0.0.1:8787";

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": resolve(import.meta.dirname, "src"),
    },
  },
  // The production build lands inside the Go module tree so the runeconsole
  // binary can go:embed it (internal/console/webdist).
  build: {
    outDir: resolve(import.meta.dirname, "../internal/console/webdist"),
    emptyOutDir: true,
  },
  // Dev: proxy the BFF namespaces to one origin (default: the Go backend) so
  // the Vite dev server and the target share an origin (no CORS needed).
  server: {
    proxy: {
      "/api": { target: proxyTarget, changeOrigin: true },
      "/console": { target: proxyTarget, changeOrigin: true },
      "/auth": { target: proxyTarget, changeOrigin: true },
    },
  },
  test: {
    environment: "jsdom",
    globals: false,
    setupFiles: ["./src/test/setup.ts"],
  },
});
