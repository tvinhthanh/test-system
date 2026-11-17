-- Owned by autotest (QĐ-18): a draft of runnable cases records the provider
-- that wrote it, beside the model ('' while queued). Every draft before this
-- one was Anthropic's.
ALTER TABLE auto_drafts ADD COLUMN provider text NOT NULL DEFAULT '' CHECK (length(provider) <= 32);
UPDATE auto_drafts SET provider = 'anthropic' WHERE model <> '';
