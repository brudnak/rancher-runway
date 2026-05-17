import { defineConfig } from "vite";
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
  plugins: [cleanGeneratedDist()],
  build: {
    emptyOutDir: false,
  },
});
