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
        path: "/invite",
        component: () => import("../../features/auth/InvitePage.vue"),
      },
      {
        path: "/forgot",
        component: () => import("../../features/auth/PasswordPage.vue"),
      },
      {
        path: "/reset",
        component: () => import("../../features/auth/PasswordPage.vue"),
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
            path: "p/:project/roi",
            component: () => import("../../features/testmgmt/RoiPage.vue"),
          },
          {
            path: "p/:project/tokens",
            component: () => import("../../features/tokens/TokensPage.vue"),
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
            path: "p/:project/specialized-results",
            component: () =>
              import("../../features/testmgmt/SpecializedResultsPage.vue"),
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
            path: "p/:project/start",
            component: () =>
              import("../../features/testmgmt/StartTestingPage.vue"),
          },
          {
            path: "p/:project/plans/:plan/run",
            component: () => import("../../features/testmgmt/RunPlanPage.vue"),
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
            path: "p/:project/url-checks",
            component: () =>
              import("../../features/testmgmt/UrlChecksPage.vue"),
          },
          {
            path: "p/:project/url-checks/:check",
            component: () =>
              import("../../features/testmgmt/UrlCheckDetailPage.vue"),
          },
          {
            path: "p/:project/auto",
            component: () => import("../../features/autotest/AutoTestPage.vue"),
          },
          {
            path: "p/:project/auto/runs/:run",
            component: () => import("../../features/autotest/AutoRunPage.vue"),
          },
          {
            path: "p/:project/auto/scans/:scan",
            component: () =>
              import("../../features/autotest/SiteScanPage.vue"),
          },
          {
            path: "p/:project/guide",
            component: () => import("../../features/guide/TesterGuidePage.vue"),
          },
          {
            path: "team",
            component: () => import("../../features/members/TeamPage.vue"),
          },
          {
            path: "team/organizations/:organization",
            component: () => import("../../features/members/MembersPage.vue"),
          },
          {
            path: "team/organizations/:organization/ai",
            component: () =>
              import("../../features/members/AiSettingsPage.vue"),
          },
          {
            path: "team/organizations/:organization/usage",
            component: () => import("../../features/billing/UsagePage.vue"),
          },
          {
            path: "team/workspaces/:workspace",
            component: () => import("../../features/members/MembersPage.vue"),
          },
          {
            path: "account/security",
            component: () => import("../../features/account/SecurityPage.vue"),
          },
          {
            path: "p/:project/members",
            component: () => import("../../features/members/MembersPage.vue"),
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
    // §11.2: an org owner without MFA reaches nothing else (the api agrees:
    // 403 MFA_ENROLLMENT_REQUIRED), so go straight to the setup.
    const mfa = session.user?.mfa;
    if (
      to.meta.protected &&
      mfa?.required &&
      !mfa.enabled &&
      to.path !== "/account/security"
    )
      return "/account/security";
    if (to.path === "/sign-in" && session.user) return "/";
    return true;
  });
  return router;
}
