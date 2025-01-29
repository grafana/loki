import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  base: "/dataobj/explorer/",
  css: {
    postcss: "./postcss.config.js",
  },
  build: {
    outDir: "../dist",
    emptyOutDir: true,
    cssCodeSplit: false,
  },
  server: {
    proxy: {
      "/dataobj/explorer/api": "http://localhost:3100",
    },
  },
});
