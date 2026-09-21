import { createRouter, createWebHistory, type RouterHistory } from "vue-router";
import type { Services } from "../providers/services";

export function makeRouter(
  session: Services["session"],
  history: RouterHistory = createWebHistory(),
) {
  const router = createRouter({
    history,
    routes: [
      {
        path: "/sign-in",
        component: () => import("../../features/auth/SignInPage.vue"),
      },
      {
        path: "/",
        component: () => import("../AppShell.vue"),
        meta: { protected: true },
        children: [
          {
            path: "",
            component: () => import("../../features/reports/ProjectsPage.vue"),
          },
          {
            path: "p/:project/overview",
            component: () => import("../../features/reports/OverviewPage.vue"),
          },
          {
            path: "p/:project/runs",
            component: () => import("../../features/reports/RunsPage.vue"),
          },
          {
            path: "p/:project/runs/:run",
            component: () => import("../../features/reports/RunDetailPage.vue"),
          },
          {
            path: "p/:project/runs/:run/results/:result",
            component: () =>
              import("../../features/reports/ResultDetailPage.vue"),
          },
          {
            path: "p/:project/tests",
            component: () =>
              import("../../features/reports/IdentitiesPage.vue"),
          },
          {
            path: "p/:project/flaky",
            component: () =>
              import("../../features/reports/IdentitiesPage.vue"),
          },
          {
            path: "p/:project/review",
            component: () =>
              import("../../features/reports/ReviewQueuePage.vue"),
          },
          {
            path: "p/:project/cases",
            component: () =>
              import("../../features/testmgmt/TestCasesPage.vue"),
          },
          {
            path: "p/:project/cases/:case",
            component: () =>
              import("../../features/testmgmt/TestCaseDetailPage.vue"),
          },
          {
            path: "p/:project/requirements",
            component: () =>
              import("../../features/testmgmt/RequirementsPage.vue"),
          },
          {
            path: "p/:project/plans",
            component: () => import("../../features/testmgmt/PlansPage.vue"),
          },
          {
            path: "p/:project/plans/:plan",
            component: () =>
              import("../../features/testmgmt/PlanDetailPage.vue"),
          },
          {
            path: "p/:project/releases",
            component: () => import("../../features/testmgmt/ReleasesPage.vue"),
          },
          {
            path: "p/:project/releases/:release",
            component: () =>
              import("../../features/testmgmt/ReleaseDetailPage.vue"),
          },
          {
            path: "p/:project/tests/:identity/history",
            component: () =>
              import("../../features/reports/IdentityHistoryPage.vue"),
          },
        ],
      },
      {
        path: "/:pathMatch(.*)*",
        component: () => import("../NotFoundPage.vue"),
      },
    ],
    scrollBehavior: () => ({ top: 0 }),
  });
  router.beforeEach(async (to) => {
    if (to.meta.protected || to.path === "/sign-in") await session.ensure();
    if (to.meta.protected && !session.user)
      return { path: "/sign-in", query: { redirect: to.fullPath } };
    if (to.path === "/sign-in" && session.user) return "/";
    return true;
  });
  return router;
}
