-- 0004_identity — tables owned by internal/identity (SPEC §6.9.5, §8.5, §9).
--
-- pg_trgm is installed into public so every schema (including the per-test
-- schemas pgtest creates) shares one copy. It is a trusted extension since
-- PostgreSQL 13, so the migrating role needs CREATE on the database, not
-- superuser. The three partial indexes at the bottom are N47: without them
-- every unknown fingerprint scans the project's active identities
-- (measured 8.3–30.5 s at 25k tests).

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE TABLE test_identities (
    id                   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id           uuid        NOT NULL,
    status               text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'merged', 'retired')),
    merged_into_id       uuid        REFERENCES test_identities (id),
    framework            text        NOT NULL,
    canonical_file_path  text        NOT NULL,
    canonical_title_path text[]      NOT NULL,
    canonical_leaf_title text        NOT NULL,                     -- N47: a real column so GIN can index it
    param_signature      text        NOT NULL DEFAULT '',
    template_key         text        NOT NULL,                     -- §9.5 grouping, N35 normalised; never an identity
    current_fingerprint  text        NOT NULL,
    first_seen_at        timestamptz NOT NULL DEFAULT now(),
    last_seen_at         timestamptz NOT NULL DEFAULT now(),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CHECK ((status = 'merged') = (merged_into_id IS NOT NULL))
);
-- §8.5 + §9.7 last row: two runs racing on the same unknown fingerprint
-- create exactly one identity.
CREATE UNIQUE INDEX test_identities_active_fingerprint ON test_identities (project_id, current_fingerprint) WHERE status = 'active';
CREATE INDEX test_identities_template ON test_identities (template_key);
-- §8.9 candidate filters (§9.3 step 2), one per condition.
CREATE INDEX test_identities_file ON test_identities (project_id, canonical_file_path) WHERE status = 'active';
CREATE INDEX test_identities_leaf ON test_identities (project_id, canonical_leaf_title) WHERE status = 'active';
CREATE INDEX test_identities_leaf_trgm ON test_identities USING gin (canonical_leaf_title public.gin_trgm_ops) WHERE status = 'active';

CREATE TABLE test_identity_aliases (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id uuid        NOT NULL REFERENCES test_identities (id),
    project_id  uuid        NOT NULL,
    fingerprint text        NOT NULL,
    source      text        NOT NULL CHECK (source IN ('exact', 'auto_match', 'manual_merge', 'manual_split')),
    match_score numeric(6, 4),
    signals     jsonb,                                             -- the raw signals that created this alias; SplitFrom copies them
    created_by  text,                                              -- actor label: user id, "system", or an operator email
    valid_from  timestamptz NOT NULL DEFAULT now(),
    valid_to    timestamptz,                                       -- NULL = currently in force (§8.5)
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (valid_to IS NULL OR valid_to > valid_from)
);
-- The hot path (§9.3 step 1): one alias in force per fingerprint.
CREATE UNIQUE INDEX test_identity_aliases_current ON test_identity_aliases (project_id, fingerprint) WHERE valid_to IS NULL;
-- Historical backfill picks the alias whose interval contains the event time.
CREATE INDEX test_identity_aliases_history ON test_identity_aliases (project_id, fingerprint, valid_from, valid_to);
CREATE INDEX test_identity_aliases_identity ON test_identity_aliases (identity_id);

-- §9.3 step 4 middle row: pairs a human has to look at. P1-min reads it as
-- CSV (§4.4); every decision is ground truth for recalibrating the weights.
CREATE TABLE review_queue_items (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid        NOT NULL,
    identity_id     uuid        NOT NULL REFERENCES test_identities (id),   -- the new identity created "now"
    candidate_id    uuid        NOT NULL REFERENCES test_identities (id),   -- the suggested rename source
    fingerprint     text        NOT NULL,
    score           numeric(6, 4) NOT NULL,
    runner_up_id    uuid        REFERENCES test_identities (id),
    runner_up_score numeric(6, 4),
    gap             numeric(6, 4) NOT NULL,
    breakdown       jsonb       NOT NULL,
    run_id          uuid,                                          -- the run that surfaced it; no FK across modules
    status          text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'merged', 'kept')),
    decided_by      text,
    decided_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_queue_pending ON review_queue_items (project_id, created_at) WHERE status = 'pending';

-- §9.4 / §11.7: destructive identity operations are logged, always.
CREATE TABLE identity_audit (
    id         bigserial   PRIMARY KEY,
    project_id uuid        NOT NULL,
    action     text        NOT NULL CHECK (action IN ('merge', 'undo_merge', 'split', 'review')),
    subject_id uuid        NOT NULL,
    target_id  uuid,
    actor      text        NOT NULL,
    details    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX identity_audit_subject ON identity_audit (subject_id, at);
