import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";

export default defineConfig(({ command, mode }) => {
  if (command === "build" && mode === "fixture")
    throw new Error("Fixture mode is development only.");
  return {
    plugins: [vue()],
    server: { port: 5173, proxy: { "/v1": "http://127.0.0.1:8080" } },
    build: { manifest: true, chunkSizeWarningLimit: 200 },
    test: {
      environment: "jsdom",
      include: ["tests/**/*.test.ts"],
      restoreMocks: true,
    },
  };
});
