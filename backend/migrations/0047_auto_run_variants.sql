-- Owned by autotest (QĐ-19 tier 2): the compatibility matrix. A run names
-- the browser × device variants its cases run on ('{}' = the one default,
-- Chromium on a desktop, as every run before this one); each case runs once
-- per variant, so a run case is keyed by its variant too. '' is the
-- default variant: the same test identity a run had before the matrix.
ALTER TABLE auto_runs ADD COLUMN variants text[] NOT NULL DEFAULT '{}' CHECK (cardinality(variants) <= 9);
ALTER TABLE auto_run_cases ADD COLUMN variant text NOT NULL DEFAULT ''
    CHECK (variant = '' OR variant ~ '^(chromium|firefox|webkit)/(desktop|phone|tablet)$');
ALTER TABLE auto_run_cases DROP CONSTRAINT auto_run_cases_pkey, ADD PRIMARY KEY (run_id, case_id, variant);
