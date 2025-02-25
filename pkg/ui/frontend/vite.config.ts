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
          // Core React libraries
          "react-core": ["react", "react-dom"],

          // Routing related
          "react-router": ["react-router-dom", "use-react-router-breadcrumbs"],

          // Form and validation libraries
          "form-libs": ["react-hook-form", "@hookform/resolvers", "zod"],

          // Radix UI components - split into smaller chunks
          "radix-core": [
            "@radix-ui/react-slot",
            "@radix-ui/react-label",
            "@radix-ui/react-tooltip",
            "@radix-ui/react-popover",
          ],
          "radix-navigation": [
            "@radix-ui/react-dropdown-menu",
            "@radix-ui/react-tabs",
            "@radix-ui/react-scroll-area",
          ],
          "radix-inputs": [
            "@radix-ui/react-checkbox",
            "@radix-ui/react-switch",
            "@radix-ui/react-select",
            "@radix-ui/react-toggle",
            "@radix-ui/react-toggle-group",
          ],
          "radix-layout": [
            "@radix-ui/react-dialog",
            "@radix-ui/react-collapsible",
            "@radix-ui/react-separator",
            "@radix-ui/react-hover-card",
          ],

          // Data visualization and charts
          "data-viz": ["recharts"],

          // Date handling
          "date-utils": ["date-fns", "react-datepicker"],

          // Icons and UI utilities
          "ui-icons": ["lucide-react", "react-icons"],
          "ui-utils": ["class-variance-authority", "clsx", "tailwind-merge"],

          // Theme and styling
          "theme-utils": ["next-themes", "tailwindcss-animate"],

          // Query management
          "query-management": ["@tanstack/react-query"],
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
