import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [vue()],
  build: {
    emptyOutDir: false,
    outDir: resolve(__dirname, "../static"),
    rollupOptions: {
      input: resolve(__dirname, "src/control_panel_header.js"),
      output: {
        entryFileNames: "control_panel_header_vue.js",
        inlineDynamicImports: true,
      },
    },
  },
});
