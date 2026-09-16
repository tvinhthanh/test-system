import { effectScope } from "vue";
import { it, expect } from "vitest";
import { useResource } from "../src/core/state/useResource";

it("new project wins when an old request finishes later", async () => {
  const scope = effectScope();
  const resource = scope.run(() => useResource<string>())!;
  let finish!: (value: string) => void;
  let firstSignal!: AbortSignal;
  const first = resource.load((signal) => {
    firstSignal = signal;
    return new Promise((resolve) => {
      finish = resolve;
    });
  });
  await resource.load(async () => "project B");
  finish("project A");
  await first;
  expect(firstSignal.aborted).toBe(true);
  expect(resource.data.value).toBe("project B");
  expect(resource.loading.value).toBe(false);
  scope.stop();
});
it("unmount aborts the request and prevents disposed state updates", async () => {
  const scope = effectScope();
  const resource = scope.run(() => useResource<string>())!;
  let finish!: (value: string) => void;
  let signal!: AbortSignal;
  const pending = resource.load((s) => {
    signal = s;
    return new Promise((r) => {
      finish = r;
    });
  });
  scope.stop();
  finish("old");
  await pending;
  expect(signal.aborted).toBe(true);
  expect(resource.data.value).toBeNull();
});
