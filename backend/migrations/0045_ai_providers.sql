-- Owned by aisuggest (QĐ-18): an organization's AI settings name the
-- provider whose key pays, not only the model — Anthropic, OpenAI, Google,
-- xAI, Mistral, DeepSeek, Groq, OpenRouter, or the operator's own
-- OpenAI-compatible endpoint. Which providers exist and which models each
-- takes is platform/llm's list, checked by the service; the database holds
-- only the shape, so a provider's new model needs no migration.
--
-- Every row before this one is Anthropic's: the only provider there was.
ALTER TABLE ai_settings ADD COLUMN provider text NOT NULL DEFAULT 'anthropic' CHECK (provider ~ '^[a-z][a-z0-9-]{0,31}$');
ALTER TABLE ai_settings DROP CONSTRAINT ai_settings_model_check;
ALTER TABLE ai_settings ADD CONSTRAINT ai_settings_model_check CHECK (model ~ '^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,199}$');

-- A suggestion records the provider that wrote its drafts, beside the model
-- ('' while queued).
ALTER TABLE case_suggestions ADD COLUMN provider text NOT NULL DEFAULT '' CHECK (length(provider) <= 32);
UPDATE case_suggestions SET provider = 'anthropic' WHERE model <> '';
