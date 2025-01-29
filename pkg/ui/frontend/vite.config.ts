import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

export default defineConfig({
  plugins: [react()],
  base: "/ui/",
  css: {
    postcss: "./postcss.config.js",
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    cssCodeSplit: false,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/ui/api/": "http://localhost:3100",
      "/loki/api/": "http://localhost:3100",
    },
  },
});
