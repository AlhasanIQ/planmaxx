import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

const outDir = fileURLToPath(new URL("../internal/review/static", import.meta.url));

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: "/",
  server: {
    port: 5173,
    proxy: {
      // Backend port for `bun run dev` proxy. Override with PLANMAXX_BACKEND
      // (e.g. PLANMAXX_BACKEND=http://127.0.0.1:4790 bun run dev) when 4790
      // is taken.
      "/api": process.env.PLANMAXX_BACKEND ?? "http://127.0.0.1:4790",
    },
  },
  build: {
    outDir,
    emptyOutDir: true,
    assetsDir: "assets",
    rollupOptions: {
      output: {
        entryFileNames: "assets/app.js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: (info) => {
          if (info.name && info.name.endsWith(".css")) {
            return "assets/app.css";
          }
          return "assets/[name][extname]";
        },
      },
    },
  },
});
