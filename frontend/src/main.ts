import { createApp, watch } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import { ApiClient, type Transport } from "./core/api/client";
import { SessionRepository } from "./features/auth/sessionRepository";
import { createSessionStore } from "./features/auth/sessionStore";
import { ReportsRepository } from "./features/reports/reportsRepository";
import { TestmgmtRepository } from "./features/testmgmt/testmgmtRepository";
import { servicesKey } from "./app/providers/services";
import { makeRouter } from "./app/router";
import "./shared/theme.css";

async function start() {
  let transport: Transport = (input, init) => fetch(input, init);
  const fixture = import.meta.env.DEV && import.meta.env.MODE === "fixture";
  if (import.meta.env.DEV && import.meta.env.MODE === "fixture") {
    transport = (
      await import("./dev/fixtureTransport")
    ).createFixtureTransport();
  }
  const app = createApp(App),
    pinia = createPinia();
  app.use(pinia);
  const api = new ApiClient(transport);
  const session = createSessionStore(api, new SessionRepository(api))(pinia);
  const router = makeRouter(session);
  app.provide(servicesKey, {
    session,
    reports: new ReportsRepository(api),
    testmgmt: new TestmgmtRepository(api),
    fixture,
  });
  app.use(router);
  watch(
    () => session.user,
    (user, previous) => {
      if (!user && previous && router.currentRoute.value.meta.protected)
        void router.replace({
          path: "/sign-in",
          query: { redirect: router.currentRoute.value.fullPath },
        });
    },
  );
  await router.isReady();
  app.mount("#app");
}
void start();
