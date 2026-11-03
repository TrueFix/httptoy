import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: "/",
  server: { proxy: { "/anything": "http://127.0.0.1:8000", "/status": "http://127.0.0.1:8000", "/sessions": "http://127.0.0.1:8000", "/metrics": "http://127.0.0.1:8000", "/json": "http://127.0.0.1:8000" } },
  build: { outDir: "../templates/web", emptyOutDir: true, sourcemap: false },
});
