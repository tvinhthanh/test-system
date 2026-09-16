import { onScopeDispose, shallowRef, ref } from "vue";

export function useResource<T>() {
  const data = shallowRef<T | null>(null);
  const error = shallowRef<unknown>(null);
  const loading = ref(false);
  let generation = 0;
  let controller: AbortController | undefined;
  let disposed = false;
  function cancel() {
    generation++;
    controller?.abort();
  }
  async function load(fetcher: (signal: AbortSignal) => Promise<T>) {
    if (disposed) return;
    cancel();
    const ticket = generation;
    controller = new AbortController();
    data.value = null;
    error.value = null;
    loading.value = true;
    try {
      const result = await fetcher(controller.signal);
      if (!disposed && ticket === generation) data.value = result;
    } catch (e) {
      if (!disposed && ticket === generation) error.value = e;
    } finally {
      if (!disposed && ticket === generation) loading.value = false;
    }
  }
  onScopeDispose(() => {
    disposed = true;
    cancel();
  });
  return { data, error, loading, load };
}
