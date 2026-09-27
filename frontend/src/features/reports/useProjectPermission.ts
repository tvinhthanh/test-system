import { computed } from "vue";
import { useRoute } from "vue-router";
import { useServices } from "../../app/providers/services";
import type { ProjectAction } from "./reportsRepository";

export function useProjectPermission(action: ProjectAction) {
  const route = useRoute();
  const { projectAccess } = useServices();
  return computed(() =>
    projectAccess.can(String(route.params.project ?? ""), action),
  );
}
