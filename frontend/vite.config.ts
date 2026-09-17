import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";

export default defineConfig(({ command, mode }) => {
  if (command === "build" && mode === "fixture")
    throw new Error("Fixture mode is development only.");
  return {
    plugins: [vue()],
    // ATP_API points the dev proxy at another api (a soak's, a staging box) for
    // scripts/live-check.mjs; default is the local `go run ./cmd/api`.
    server: {
      port: 5173,
      proxy: { "/v1": process.env.ATP_API || "http://127.0.0.1:8080" },
    },
    build: { manifest: true, chunkSizeWarningLimit: 200 },
    test: {
      environment: "jsdom",
      include: ["tests/**/*.test.ts"],
      restoreMocks: true,
    },
  };
});
