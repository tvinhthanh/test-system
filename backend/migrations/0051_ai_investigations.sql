-- Owned by triage (QĐ-4): bounded, human-reviewed AI failure investigations.
CREATE TABLE ai_investigations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid NOT NULL,
    org_id          uuid NOT NULL,
    test_result_id  uuid NOT NULL,
    stable_test_id  uuid,
    dedupe_key      text NOT NULL CHECK (length(dedupe_key) = 64),
    requested_day   date NOT NULL,
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued','collecting','investigating','done','error')),
    requested_by    text NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 254),
    provider        text NOT NULL DEFAULT '',
    model           text NOT NULL DEFAULT '',
    key_source      text NOT NULL DEFAULT '',
    prompt_version  text NOT NULL DEFAULT 'investigation-v1',
    turn_count      integer NOT NULL DEFAULT 0 CHECK (turn_count BETWEEN 0 AND 4),
    tool_count      integer NOT NULL DEFAULT 0 CHECK (tool_count BETWEEN 0 AND 6),
    input_tokens    integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens   integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    hypothesis_id   uuid,
    error           text NOT NULL DEFAULT '' CHECK (octet_length(error) <= 1000),
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    UNIQUE (project_id, dedupe_key, requested_day)
);
CREATE INDEX ai_investigations_result ON ai_investigations (project_id, test_result_id, created_at DESC);
CREATE INDEX ai_investigations_org_day ON ai_investigations (org_id, requested_day);

CREATE TABLE ai_investigation_steps (
    investigation_id uuid NOT NULL,
    sequence         integer NOT NULL CHECK (sequence BETWEEN 1 AND 12),
    kind             text NOT NULL CHECK (kind IN ('tool','model')),
    tool             text NOT NULL DEFAULT '',
    request          jsonb NOT NULL DEFAULT '{}'::jsonb,
    observation      jsonb NOT NULL DEFAULT '{}'::jsonb,
    input_tokens     integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens    integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (investigation_id, sequence)
);

ALTER TABLE ai_hypotheses ADD COLUMN investigation_id uuid;
CREATE UNIQUE INDEX ai_hypotheses_investigation ON ai_hypotheses (investigation_id) WHERE investigation_id IS NOT NULL;
CREATE UNIQUE INDEX triage_events_hypothesis_decision ON triage_events (hypothesis_id)
    WHERE type IN ('HYPOTHESIS_ACCEPTED','HYPOTHESIS_REJECTED');
