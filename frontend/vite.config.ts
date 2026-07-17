/// <reference types="vitest/config" />
import { resolve } from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

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
  // Dev: proxy the BFF namespaces to the loopback console listener so the
  // Vite dev server and the Go backend share one origin (no CORS needed).
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8787",
      "/console": "http://127.0.0.1:8787",
      "/auth": "http://127.0.0.1:8787",
    },
  },
  test: {
    environment: "jsdom",
    globals: false,
    setupFiles: ["./src/test/setup.ts"],
  },
});
