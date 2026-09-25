/// <reference types="vitest" />
import { defineConfig } from "vitest/config";

// Vitest for the frontend workspace (D24, ARC-9 S3): the Yjs collab client
// engine under frontend/js/features/ide-react/collab and its Node-testable
// suites. The services/web vitest runner cannot resolve this workspace's
// dependencies (PnP is per-workspace), so the engine tests run here.
export default defineConfig({
  resolve: {
    alias: {
      "@": new URL("../js", import.meta.url).pathname,
    },
  },
  esbuild: {
    jsx: "automatic",
  },
  test: {
    name: "CollabYjs",
    environment: "node",
    globals: false,
    include: ["js/features/ide-react/collab/**/*.test.ts"],
    fileParallelism: true,
    testTimeout: 20_000,
  },
});
