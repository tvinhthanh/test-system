import { inject, type InjectionKey } from "vue";
import type { ReportsRepository } from "../../features/reports/reportsRepository";
import type { ProjectAccess } from "../../features/reports/projectAccess";
import type { TestmgmtRepository } from "../../features/testmgmt/testmgmtRepository";
import type { createSessionStore } from "../../features/auth/sessionStore";

export interface Services {
  reports: ReportsRepository;
  projectAccess: ProjectAccess;
  testmgmt: TestmgmtRepository;
  session: ReturnType<ReturnType<typeof createSessionStore>>;
  fixture: boolean;
}
export const servicesKey: InjectionKey<Services> = Symbol("services");
export function useServices() {
  const services = inject(servicesKey);
  if (!services) throw new Error("Missing application services");
  return services;
}
