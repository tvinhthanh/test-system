import { onScopeDispose, ref, shallowRef, watch, type Ref } from "vue";

// One request owner for a route detail. Route changes and hidden tabs cancel
// in-flight polls; temporary poll errors back off without erasing the last
// good answer. The first failed request waits for an explicit retry.
export function usePolledDetail<T>(
  project: Ref<string>,
  id: Ref<string>,
  fetcher: (project: string, id: string, signal: AbortSignal) => Promise<T>,
  pending: (value: T) => boolean,
) {
  const data = shallowRef<T | null>(null);
  const error = shallowRef<unknown>(null);
  const pollError = shallowRef<unknown>(null);
  const loading = ref(false);
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let generation = 0;
  let delay = 3000;
  let disposed = false;

  function stop() {
    generation++;
    clearTimeout(timer);
    controller?.abort();
    controller = undefined;
  }
  function schedule() {
    clearTimeout(timer);
    if (!disposed && data.value && pending(data.value) && !document.hidden)
      timer = setTimeout(() => void request(false), delay);
  }
  async function request(reset: boolean) {
    if (disposed) return;
    stop();
    if (!project.value || !id.value) {
      data.value = null;
      error.value = null;
      pollError.value = null;
      loading.value = false;
      return;
    }
    const ticket = generation;
    const p = project.value;
    const key = id.value;
    controller = new AbortController();
    if (reset) {
      data.value = null;
      error.value = null;
      pollError.value = null;
      loading.value = true;
      delay = 3000;
    }
    try {
      const answer = await fetcher(p, key, controller.signal);
      if (disposed || ticket !== generation) return;
      data.value = answer;
      error.value = null;
      pollError.value = null;
      delay = 3000;
    } catch (cause) {
      if (disposed || ticket !== generation) return;
      if (data.value) {
        pollError.value = cause;
        delay = Math.min(delay * 2, 30000);
      } else error.value = cause;
    } finally {
      if (ticket === generation && !disposed) {
        loading.value = false;
        controller = undefined;
        schedule();
      }
    }
  }
  function replace(answer: T) {
    stop();
    data.value = answer;
    error.value = null;
    pollError.value = null;
    delay = 3000;
    schedule();
  }
  function visibility() {
    if (document.hidden) stop();
    else if (!data.value || pending(data.value)) void request(false);
  }
  watch([project, id], () => void request(true), { immediate: true });
  document.addEventListener("visibilitychange", visibility);
  onScopeDispose(() => {
    disposed = true;
    stop();
    document.removeEventListener("visibilitychange", visibility);
  });
  return {
    data,
    error,
    pollError,
    loading,
    reload: () => request(true),
    replace,
  };
}
