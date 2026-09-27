import { reactive } from "vue";
import type { Page } from "../../core/api/decode";
import type {
  Project,
  ProjectAction,
  ReportsRepository,
} from "./reportsRepository";

// Permission metadata comes from the server's authz matrix. This cache lets a
// deep-linked page resolve its project without teaching views about roles.
export class ProjectAccess {
  private projects = reactive(new Map<string, Project>());
  private complete = false;
  private loading: Promise<void> | null = null;

  constructor(private reports: ReportsRepository) {}

  // A page reached by cursor says nothing about earlier pages, so only the
  // full scan in loadUntil may mark the cache complete.
  remember(page: Page<Project>) {
    for (const project of page.items) this.projects.set(project.id, project);
  }

  // Actions belong to the signed-in user; drop them when that user changes.
  clear() {
    this.projects.clear();
    this.complete = false;
    this.loading = null;
  }

  async page(cursor: string, signal: AbortSignal) {
    const page = await this.reports.projects(cursor, signal);
    this.remember(page);
    return page;
  }

  async ensure(projectId: string) {
    if (!projectId || this.projects.has(projectId) || this.complete) return;
    if (!this.loading) {
      this.loading = this.loadUntil(projectId).finally(() => {
        this.loading = null;
      });
    }
    await this.loading;
    if (!this.projects.has(projectId) && !this.complete)
      await this.ensure(projectId);
  }

  can(projectId: string, action: ProjectAction) {
    return this.projects.get(projectId)?.actions.includes(action) ?? false;
  }

  private async loadUntil(projectId: string) {
    let cursor = "";
    do {
      const page = await this.reports.projects(
        cursor,
        new AbortController().signal,
      );
      this.remember(page);
      if (this.projects.has(projectId)) return;
      cursor = page.nextCursor ?? "";
    } while (cursor);
    this.complete = true;
  }
}
