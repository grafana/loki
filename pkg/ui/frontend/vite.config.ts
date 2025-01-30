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
    rollupOptions: {
      output: {
        manualChunks: {
          // React core libraries
          "react-core": ["react", "react-dom", "react-router-dom"],

          // Radix UI components
          "radix-ui": [
            "@radix-ui/react-collapsible",
            "@radix-ui/react-dialog",
            "@radix-ui/react-dropdown-menu",
            "@radix-ui/react-hover-card",
            "@radix-ui/react-label",
            "@radix-ui/react-popover",
            "@radix-ui/react-scroll-area",
            "@radix-ui/react-select",
            "@radix-ui/react-separator",
            "@radix-ui/react-slot",
            "@radix-ui/react-switch",
            "@radix-ui/react-tabs",
            "@radix-ui/react-tooltip",
          ],

          // Data visualization and charts
          "data-viz": ["recharts"],

          // Icons and UI utilities
          "ui-utils": [
            "lucide-react",
            "react-icons",
            "class-variance-authority",
            "clsx",
            "tailwind-merge",
          ],

          // Date handling and other utilities
          utils: ["date-fns", "next-themes"],
        },
      },
    },
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
