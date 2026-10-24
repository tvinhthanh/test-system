-- Owned by testmgmt. The kind of testing a plan is for (QĐ-15): the start
-- flow asks for it first, suggests cases by it, and the run screen offers a
-- URL check for the kinds with an automatic part. The values are the case
-- taxonomy's usage tags and purposes (migration 0033). NULL: a plan made
-- before this, or without choosing.
ALTER TABLE test_plans ADD COLUMN test_type text CHECK (test_type IN (
    'smoke', 'regression', 'e2e',
    'functional', 'performance', 'security', 'usability', 'accessibility', 'compatibility', 'recovery'));
