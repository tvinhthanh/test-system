-- 0008_triage — tables owned by internal/triage (SPEC §6.9.5, §8.3, §8.7).
--
-- TriageEvent is the single source of truth for TTRC (§8.3): nothing
-- updates a triage metric except an event row here. Append-only. P1-min
-- writes events by hand (cmd/admin); P1.5 adds AI hypotheses in
-- ai_hypotheses, referenced by hypothesis_id.

CREATE TABLE triage_events (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    test_result_id uuid        NOT NULL,                             -- results' table; no FK across modules
    run_id         uuid        NOT NULL,
    type           text        NOT NULL CHECK (type IN (
                       'FAILURE_SEEN', 'HYPOTHESIS_PROPOSED', 'HYPOTHESIS_ACCEPTED', 'HYPOTHESIS_REJECTED',
                       'MARKED_FLAKY', 'MARKED_KNOWN_ISSUE', 'ASSIGNED', 'RESOLVED')),
    actor_type     text        NOT NULL CHECK (actor_type IN ('USER', 'AI', 'SYSTEM')),
    actor_id       text,                                             -- user id, or a model/prompt label for AI
    hypothesis_id  uuid,
    payload        jsonb       NOT NULL DEFAULT '{}'::jsonb,         -- category, assignee, note, ...
    occurred_at    timestamptz NOT NULL DEFAULT now(),
    created_at     timestamptz NOT NULL DEFAULT now()
);
-- §8.9 TTRC dashboard: events of one result by type and time.
CREATE INDEX triage_events_result ON triage_events (test_result_id, type, occurred_at);
-- untriaged_rate / triage_coverage per project over a window.
CREATE INDEX triage_events_project_time ON triage_events (project_id, occurred_at);

-- AiHypothesis (§8.7), created empty now so P1.5 is an INSERT, not a
-- migration on a live table.
CREATE TABLE ai_hypotheses (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       uuid        NOT NULL,
    test_result_id   uuid        NOT NULL,
    category         text        NOT NULL CHECK (category IN ('app_bug', 'test_bug', 'flaky_timing', 'env_infra', 'env_data', 'dependency_external', 'unknown')),
    confidence       numeric(4, 3) NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    evidence         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    suggested_action text,
    model            text        NOT NULL,
    prompt_version   text        NOT NULL,
    cost_usd         numeric(10, 6) NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_hypotheses_result ON ai_hypotheses (test_result_id, created_at);
