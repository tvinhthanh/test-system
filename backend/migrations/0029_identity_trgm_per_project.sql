-- 0029_identity_trgm_per_project — owned by internal/identity (SPEC §8.9,
-- §9.3 step 2, N47).
--
-- The trigram index of 0004 covers every tenant's titles: `leaf % $title`
-- matched across all projects and only then was project_id applied, so a
-- new test's candidate search got slower with every customer — and common
-- titles ("should login") are common everywhere. Measured 07-10 (20
-- projects × 5 000 identities, warm): 31 ms per unknown fingerprint
-- through the old indexes, 14 ms through this one, which keys the trigram
-- GIN by project first. btree_gin is what lets a GIN index carry the uuid
-- column; it ships with PostgreSQL contrib, like pg_trgm, and lives in
-- public for the same reason (every schema sees it).

CREATE EXTENSION IF NOT EXISTS btree_gin WITH SCHEMA public;

CREATE INDEX test_identities_project_leaf_trgm ON test_identities
    USING gin (project_id, canonical_leaf_title public.gin_trgm_ops) WHERE status = 'active';
DROP INDEX test_identities_leaf_trgm;
