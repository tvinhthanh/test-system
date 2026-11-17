-- Existing cases stay explicitly unclassified; do not infer from framework.
ALTER TABLE test_cases ADD COLUMN taxonomy jsonb NOT NULL DEFAULT
    '{"level":"","purposes":[],"tags":[],"executionModes":[],"performanceTypes":[]}'::jsonb;

ALTER TABLE test_cases ADD CONSTRAINT test_case_taxonomy_valid CHECK ((
    jsonb_typeof(taxonomy) = 'object'
    AND taxonomy - ARRAY['level','purposes','tags','executionModes','performanceTypes'] = '{}'::jsonb
    AND taxonomy->>'level' IN ('','unit','integration','system','acceptance')
    AND jsonb_typeof(taxonomy->'purposes') = 'array'
    AND taxonomy->'purposes' <@ '["functional","performance","security","usability","accessibility","compatibility","recovery"]'::jsonb
    AND jsonb_typeof(taxonomy->'tags') = 'array'
    AND taxonomy->'tags' <@ '["smoke","regression","e2e"]'::jsonb
    AND jsonb_typeof(taxonomy->'executionModes') = 'array'
    AND taxonomy->'executionModes' <@ '["manual","automated"]'::jsonb
    AND jsonb_typeof(taxonomy->'performanceTypes') = 'array'
    AND taxonomy->'performanceTypes' <@ '["load","stress","spike","soak","volume","scalability"]'::jsonb
    AND (taxonomy->'performanceTypes' = '[]'::jsonb OR taxonomy->'purposes' ? 'performance')
) IS TRUE);
