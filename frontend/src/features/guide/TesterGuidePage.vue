<script setup lang="ts">
// How a tester works in ATP (QĐ-14): manual testing end to end, the quick
// checks that need no test cases, and where automation plugs in. Every step
// links to the screen it describes; nothing here needs code or the ATP
// source.
import {
  Bot,
  ClipboardList,
  KeyRound,
  Play,
  ListChecks,
  Rocket,
  ScanSearch,
  Target,
  ChartNoAxesCombined,
} from "@lucide/vue";
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useProjectPermission } from "../reports/useProjectPermission";
import PageHeader from "../../shared/PageHeader.vue";
import SectionHeading from "../../shared/SectionHeading.vue";

const route = useRoute();
const p = computed(
  () => `/p/${encodeURIComponent(String(route.params.project))}`,
);
const canTokens = useProjectPermission("token.manage");
const link = "text-primary font-medium underline underline-offset-2";
</script>
<template>
  <PageHeader
    eyebrow="Project"
    title="How to test in ATP"
    description="Everything a tester needs is in this dashboard: no test code, no copy of the ATP source. Start with the steps below; each one links to its screen."
  />

  <section
    class="guide-roles bg-card mb-6 rounded-lg border p-5 text-sm"
    aria-labelledby="guide-roles"
  >
    <h2 id="guide-roles" class="mb-2 font-semibold">Who can do what</h2>
    <ul class="text-muted-foreground space-y-1">
      <li>
        <strong class="text-foreground">Viewer</strong> — reads plans, results
        and reports.
      </li>
      <li>
        <strong class="text-foreground">Member</strong> (tester) — writes test
        cases and plans, records results and evidence, runs URL checks, adds
        environments.
      </li>
      <li>
        <strong class="text-foreground">Maintainer</strong> — also decides
        releases and issues CI tokens.
      </li>
      <li>
        <strong class="text-foreground">Admins</strong> — also add people and
        projects (Members, Team).
      </li>
    </ul>
  </section>

  <SectionHeading id="guide-start" title="The quick way: Start testing" />
  <section
    class="guide-start bg-card mb-2 flex gap-3 rounded-lg border p-5 text-sm"
    aria-labelledby="guide-start"
  >
    <Play class="text-muted-foreground mt-0.5 size-4 shrink-0" />
    <div class="space-y-2">
      <p>
        <RouterLink :to="`${p}/start`" :class="link">Start testing</RouterLink>
        walks through a round in four choices:
      </p>
      <ol class="text-muted-foreground list-decimal space-y-1 pl-5">
        <li>
          <strong class="text-foreground">What</strong> — smoke, regression, end
          to end, functional, performance, security, usability, accessibility,
          compatibility or recovery.
        </li>
        <li>
          <strong class="text-foreground">Where</strong> — the environment and
          its address.
        </li>
        <li>
          <strong class="text-foreground">Which cases</strong> — the matching
          cases from your library, a starter set for that kind of testing, or
          cases drafted by AI from the site and your description (when an admin
          has set up AI under Team → organization → AI settings). Tick the ones
          to keep.
        </li>
        <li>
          <strong class="text-foreground">Start</strong> — the plan is made and
          the run screen opens: the site is one click away, each case shows its
          steps, you record each step and attach screenshots, and it moves on to
          the next case.
        </li>
      </ol>
      <p class="text-muted-foreground">
        The steps below are the same work done screen by screen.
      </p>
    </div>
  </section>

  <SectionHeading id="guide-auto-test" title="Let the server test by itself" />
  <section
    class="guide-auto-test bg-card mb-2 flex gap-3 rounded-lg border p-5 text-sm"
    aria-labelledby="guide-auto-test"
  >
    <Bot class="text-muted-foreground mt-0.5 size-4 shrink-0" />
    <div class="space-y-2">
      <p>
        In
        <RouterLink :to="`${p}/auto`" :class="link">Auto test</RouterLink>
        the server clicks through cases itself, in a real browser on your site:
      </p>
      <ol class="text-muted-foreground list-decimal space-y-1 pl-5">
        <li>
          Pick the environment. For pages behind a sign-in, add a
          <strong class="text-foreground">test account</strong> — AI finds how
          to sign in; the password never reaches it.
        </li>
        <li>
          Say where a case starts (<code v-pre>/message?to={{recipient}}</code>),
          its inputs and what to cover — AI reads the page and drafts runnable
          cases — or write the steps yourself: click, type, check.
        </li>
        <li>
          Run them. Each step passes or fails with screenshots; a step whose
          button moved can be repaired by AI (a check never is). Results land in
          a test plan and in Test runs.
        </li>
        <li>
          Scan a whole site for broken pages, or start the cases from your
          deploy pipeline with a token.
        </li>
      </ol>
      <p class="text-muted-foreground">
        Steps really happen on the site — use a staging environment.
      </p>
    </div>
  </section>

  <SectionHeading id="guide-manual" title="Manual testing, step by step" />
  <ol
    class="guide-steps bg-card space-y-5 rounded-lg border p-5 text-sm"
    aria-labelledby="guide-manual"
  >
    <li class="flex gap-3">
      <ClipboardList class="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div class="space-y-1">
        <p class="font-medium">1. Write the test cases</p>
        <p class="text-muted-foreground">
          In
          <RouterLink :to="`${p}/cases`" :class="link">Test cases</RouterLink>,
          add a case for each thing to verify: its steps, the expected result of
          each step, a priority, and whether it is critical for a release. Have
          them in a spreadsheet? Import the CSV on the same page.
        </p>
      </div>
    </li>
    <li class="flex gap-3">
      <Target class="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div class="space-y-1">
        <p class="font-medium">2. Link them to requirements (optional)</p>
        <p class="text-muted-foreground">
          <RouterLink :to="`${p}/requirements`" :class="link"
            >Requirements</RouterLink
          >
          list what the product must do; linking cases to them shows what is
          covered and what is not.
        </p>
      </div>
    </li>
    <li class="flex gap-3">
      <ListChecks class="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div class="space-y-1">
        <p class="font-medium">3. Open a test plan for the round</p>
        <p class="text-muted-foreground">
          In
          <RouterLink :to="`${p}/plans`" :class="link">Test plans</RouterLink>,
          choose <em>New plan</em>, name the round (for example “Release 2026.11
          regression”) and pick the <strong>environment</strong> — the site
          under test. <em>+ New environment</em> records a new one with its
          address, e.g. <code>https://staging.example.com</code>. Then choose
          the cases and assign them to testers.
        </p>
      </div>
    </li>
    <li class="flex gap-3">
      <ListChecks class="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div class="space-y-1">
        <p class="font-medium">4. Run the cases</p>
        <p class="text-muted-foreground">
          Open the plan and click the site's address to open it in a new tab.
          Follow each case's steps and record every step as passed, failed,
          blocked or skipped, with what actually happened.
        </p>
      </div>
    </li>
    <li class="flex gap-3">
      <ListChecks class="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div class="space-y-1">
        <p class="font-medium">5. Attach evidence and the bug</p>
        <p class="text-muted-foreground">
          On a failure, attach a screenshot or a screen recording and paste the
          link of the bug from your tracker into the result.
        </p>
      </div>
    </li>
    <li class="flex gap-3">
      <ListChecks class="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div class="space-y-1">
        <p class="font-medium">6. Retest when it is fixed</p>
        <p class="text-muted-foreground">
          Record a retest of the failed case in the same plan. The history keeps
          the failure and the retest side by side.
        </p>
      </div>
    </li>
    <li class="flex gap-3">
      <Rocket class="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div class="space-y-1">
        <p class="font-medium">7. Decide the release</p>
        <p class="text-muted-foreground">
          In
          <RouterLink :to="`${p}/releases`" :class="link">Releases</RouterLink>,
          a maintainer creates the release, evaluates its gate — read from the
          latest automated run (critical cases that regressed, failures nobody
          triaged, flaky rate) and P0 requirement coverage — and records the go
          / no-go decision with a reason. With manual testing only, the plan's
          results are the evidence for that decision.
        </p>
      </div>
    </li>
  </ol>

  <SectionHeading id="guide-quick" title="Quick checks without test cases" />
  <ul
    class="bg-card space-y-5 rounded-lg border p-5 text-sm"
    aria-labelledby="guide-quick"
  >
    <li class="flex gap-3">
      <ScanSearch class="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div class="space-y-1">
        <p class="font-medium">URL checks</p>
        <p class="text-muted-foreground">
          In
          <RouterLink :to="`${p}/url-checks`" :class="link"
            >URL checks</RouterLink
          >, type an address and a browser on the server opens it: does the page
          load, any JavaScript errors, images or links that are broken,
          accessibility problems, how fast the main content appears, whether it
          fits a phone — with desktop and phone screenshots. One page per check,
          a few seconds each.
        </p>
      </div>
    </li>
    <li class="flex gap-3">
      <ChartNoAxesCombined
        class="text-muted-foreground mt-0.5 size-4 shrink-0"
      />
      <div class="space-y-1">
        <p class="font-medium">Specialized results</p>
        <p class="text-muted-foreground">
          Ran a performance, security, accessibility, compatibility, recovery or
          usability test with another tool? Record its result and attach the
          original report in
          <RouterLink :to="`${p}/specialized-results`" :class="link"
            >Specialized results</RouterLink
          >.
        </p>
      </div>
    </li>
  </ul>

  <SectionHeading id="guide-auto" title="Automated tests" />
  <section
    class="bg-card flex gap-3 rounded-lg border p-5 text-sm"
    aria-labelledby="guide-auto"
  >
    <KeyRound class="text-muted-foreground mt-0.5 size-4 shrink-0" />
    <p class="text-muted-foreground">
      For whoever writes Playwright tests: results stream into
      <RouterLink :to="`${p}/runs`" :class="link">Test runs</RouterLink>
      through the ATP reporter, installed straight from this server.
      <template v-if="canTokens">
        <RouterLink :to="`${p}/tokens`" :class="link">CI tokens</RouterLink>
        has the token and the copy-paste setup.
      </template>
      <template v-else>
        A maintainer issues the CI token and finds the setup on the CI tokens
        page.
      </template>
    </p>
  </section>
</template>
