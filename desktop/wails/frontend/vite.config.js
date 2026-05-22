import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { rmSync } from "node:fs";
import { resolve } from "node:path";

const outDir = resolve("dist");

const cleanGeneratedDist = () => ({
  name: "clean-generated-dist",
  apply: "build",
  buildStart() {
    rmSync(resolve(outDir, "assets"), { recursive: true, force: true });
    rmSync(resolve(outDir, "index.html"), { force: true });
  },
});

export default defineConfig({
  plugins: [vue(), cleanGeneratedDist()],
  build: {
    emptyOutDir: false,
  },
});
