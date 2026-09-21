import { ref } from "vue";

// One in-flight write at a time per screen, with the server's answer kept
// for the row or form that asked. Screens never swallow a 403/409: the
// message is shown where the click happened.
export function useMutation() {
  const busy = ref<string | null>(null); // a key naming what is being written
  const error = ref<{ key: string; error: unknown } | null>(null);
  async function run<T>(key: string, fn: (signal: AbortSignal) => Promise<T>) {
    if (busy.value) return undefined;
    busy.value = key;
    error.value = null;
    try {
      return await fn(new AbortController().signal);
    } catch (e) {
      error.value = { key, error: e };
      return undefined;
    } finally {
      busy.value = null;
    }
  }
  return { busy, error, run };
}
