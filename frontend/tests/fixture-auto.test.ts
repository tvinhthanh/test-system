import { expect, it } from "vitest";
import { ApiClient } from "../src/core/api/client";
import { createFixtureTransport } from "../src/dev/fixtureTransport";
import { AutotestRepository } from "../src/features/autotest/autotestRepository";
import { MembersRepository } from "../src/features/members/membersRepository";
import {
  emptyReportData,
  reportFields,
} from "../src/features/testmgmt/specializedReports";
import { TestmgmtRepository } from "../src/features/testmgmt/testmgmtRepository";

const signal = () => new AbortController().signal;

it("previews the auto test workflow through the production DTO decoders", async () => {
  sessionStorage.removeItem("atp-fixture-session");
  const api = new ApiClient(createFixtureTransport());
  api.csrfToken = "ATP_FIXTURE_ONLY";
  const auto = new AutotestRepository(api);
  const testmgmt = new TestmgmtRepository(api);

  const environments = await testmgmt.environments("commerce", signal());
  expect(environments[0]?.baseUrl).toBe("https://example.com");
  expect((await auto.specs("commerce", signal())).items).toHaveLength(1);
  expect((await auto.runs("commerce", signal())).items).toHaveLength(1);
  expect((await auto.scans("commerce", signal())).items).toHaveLength(1);
  expect(
    (await auto.accounts("commerce", environments[0]!.id, signal()))[0]?.key,
  ).toBe("shopper");
  const account = await auto.saveAccount(
    "commerce",
    environments[0]!.id,
    {
      key: "editor",
      username: "editor@example.test",
      password: "preview-secret",
      loginUrl: "/login",
    },
    signal(),
  );
  expect(
    (
      await auto.findLogin(
        "commerce",
        environments[0]!.id,
        account.key,
        signal(),
      )
    ).loginStatus,
  ).toBe("ok");
  await auto.deleteAccount(
    "commerce",
    environments[0]!.id,
    account.key,
    signal(),
  );
  expect(
    (await auto.accounts("commerce", environments[0]!.id, signal())).some(
      (item) => item.key === account.key,
    ),
  ).toBe(false);

  const site = await testmgmt.createEnvironment(
    "commerce",
    { key: "new-site", baseUrl: "https://new.example.test" },
    signal(),
  );
  expect(
    (await testmgmt.environments("commerce", signal())).some(
      (e) => e.id === site.id,
    ),
  ).toBe(true);
  const suggestion = await testmgmt.createSuggestion(
    "commerce",
    {
      testType: "functional",
      environmentId: site.id,
      url: "",
      context: "Checkout",
      count: 2,
    },
    signal(),
  );
  expect(
    (await testmgmt.suggestion("commerce", suggestion.id, signal())).drafts,
  ).toHaveLength(2);
  const draft = await auto.createDraft(
    "commerce",
    {
      environmentId: site.id,
      target: "/",
      account: "",
      inputs: {},
      context: "Landing",
      count: 1,
      testType: "functional",
    },
    signal(),
  );
  expect((await auto.draft("commerce", draft.id, signal())).status).toBe(
    "done",
  );

  const made = await auto.createCase(
    "commerce",
    {
      title: "Landing page opens",
      priority: "p2",
      testType: "functional",
      target: "/",
      account: "",
      inputs: {},
      source: "manual",
      steps: [
        {
          do: "expect_title",
          by: "",
          role: "",
          name: "",
          value: "Example Domain",
          say: "",
          expected: "",
        },
      ],
    },
    signal(),
  );
  expect(
    (await auto.specs("commerce", signal())).items.some(
      (s) => s.caseId === made.caseId,
    ),
  ).toBe(true);
  expect(
    (
      await auto.saveSpec(
        "commerce",
        made.caseId,
        { target: "/updated", account: "", inputs: {}, steps: made.steps },
        signal(),
      )
    ).target,
  ).toBe("/updated");

  const run = await auto.createRun(
    "commerce",
    { environmentId: site.id, caseIds: [made.caseId], heal: true },
    signal(),
  );
  expect((await auto.run("commerce", run.id, signal())).cases[0]?.caseId).toBe(
    made.caseId,
  );

  const scan = await auto.createScan(
    "commerce",
    { environmentId: site.id, url: site.baseUrl, account: "", maxPages: 10 },
    signal(),
  );
  expect((await auto.scan("commerce", scan.id, signal())).checks[0]?.url).toBe(
    site.baseUrl,
  );
  await auto.deleteSpec("commerce", made.caseId, signal());
  expect(
    (await auto.specs("commerce", signal())).items.some(
      (item) => item.caseId === made.caseId,
    ),
  ).toBe(false);
});

it("previews specialized recording and membership decisions without resource errors", async () => {
  const api = new ApiClient(createFixtureTransport());
  api.csrfToken = "ATP_FIXTURE_ONLY";
  const testmgmt = new TestmgmtRepository(api);
  const members = new MembersRepository(api);
  const node = { level: "project" as const, id: "commerce" };
  expect(
    (await testmgmt.specializedResults("commerce", {}, signal())).items,
  ).toEqual([]);
  const result = await testmgmt.createSpecializedResult(
    "commerce",
    {
      schemaVersion: 1,
      idempotencyKey: crypto.randomUUID(),
      testCaseId: "",
      kind: "performance",
      title: "Load test preview",
      state: "not_run",
      notes: "Scheduled for next week",
      environment: "staging",
      applicationVersion: "v1",
      source: "manual",
      toolVersion: "1",
      adapterVersion: "atp-report-v1",
      startedAt: "0001-01-01T00:00:00Z",
      finishedAt: "0001-01-01T00:00:00Z",
      retestOf: "",
      details: emptyReportData(reportFields.performance),
      originalReport: "",
    },
    signal(),
  );
  expect(
    (await testmgmt.specializedResult("commerce", result.id, signal())).outcome,
  ).toBe("not_run");
  expect(
    (await testmgmt.specializedResults("commerce", {}, signal())).items,
  ).toHaveLength(1);

  const pending = await members.accessRequests(node, signal());
  expect(pending[0]?.email).toBe("new.member@example.test");
  await members.decideAccessRequest(
    node,
    pending[0]!.id,
    "approved",
    "viewer",
    signal(),
  );
  expect(await members.accessRequests(node, signal())).toEqual([]);
  expect(
    (await members.all(node, signal())).some(
      (person) => person.email === pending[0]!.email,
    ),
  ).toBe(true);
});

it("returns a bounded empty preview to a reader project", async () => {
  const api = new ApiClient(createFixtureTransport());
  const auto = new AutotestRepository(api);
  const testmgmt = new TestmgmtRepository(api);
  expect(await testmgmt.environments("platform", signal())).toEqual([]);
  expect((await auto.specs("platform", signal())).items).toEqual([]);
  expect((await auto.runs("platform", signal())).items).toEqual([]);
});
