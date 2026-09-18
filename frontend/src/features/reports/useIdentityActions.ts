import { computed, ref, type Ref } from "vue";
import { useServices } from "../../app/providers/services";
import { useResource } from "../../core/state/useResource";
import type { Alias, Identity } from "./reportsRepository";

// §9.4 manual tools on one identity: its aliases, merge into another,
// undo a merge, split from a point in time. The server owns every rule
// (admin only, audited, N43: undo is not split); this only sequences the
// calls and surfaces the server's answer per action.
export function useIdentityActions(
  project: Ref<string>,
  identityId: Ref<string>,
  onChanged: () => void,
) {
  const { reports } = useServices();
  const aliases = useResource<Alias[]>();
  const candidates = useResource<Identity[]>();
  const busy = ref<"merge" | "undo" | "split" | null>(null);
  const actionError = ref<unknown>(null);
  const mergeTarget = ref("");
  const splitFingerprint = ref("");
  const splitAt = ref(""); // datetime-local value

  function loadAliases() {
    return aliases.load((signal) =>
      reports.aliases(project.value, identityId.value, signal),
    );
  }
  // Merge targets: the first page of active identities other than this one.
  // Enough for the P1 queue sizes; a search box narrows it client-side.
  function loadCandidates() {
    return candidates.load(async (signal) => {
      const page = await reports.identities(project.value, "", "", signal);
      return page.items.filter((i) => i.id !== identityId.value);
    });
  }
  const inForce = computed(() =>
    (aliases.data.value ?? []).filter((a) => a.validTo === null),
  );

  async function run<T>(
    kind: "merge" | "undo" | "split",
    fn: (signal: AbortSignal) => Promise<T>,
  ): Promise<T | undefined> {
    if (busy.value) return undefined;
    busy.value = kind;
    actionError.value = null;
    try {
      const out = await fn(new AbortController().signal);
      onChanged();
      void loadAliases();
      return out;
    } catch (e) {
      actionError.value = e;
      return undefined;
    } finally {
      busy.value = null;
    }
  }
  const merge = () =>
    run("merge", (signal) =>
      reports.mergeIdentity(
        project.value,
        identityId.value,
        mergeTarget.value,
        signal,
      ),
    );
  const undo = () =>
    run("undo", (signal) =>
      reports.undoMerge(project.value, identityId.value, signal),
    );
  // datetime-local has no zone; it is read as the browser's local time and
  // sent as an RFC 3339 instant, which is what the server stores.
  const split = () =>
    run("split", (signal) =>
      reports.splitIdentity(
        project.value,
        identityId.value,
        splitFingerprint.value,
        new Date(splitAt.value).toISOString(),
        signal,
      ),
    );
  return {
    aliases,
    candidates,
    inForce,
    busy,
    actionError,
    mergeTarget,
    splitFingerprint,
    splitAt,
    loadAliases,
    loadCandidates,
    merge,
    undo,
    split,
  };
}
