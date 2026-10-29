-- Owned by tenancy. An environment carries the address of the system under
-- test (QĐ-14): a tester opening a plan sees which site to test, and a URL
-- check (urlcheck) starts from it. NULL: no address recorded (every
-- environment a reporter created before this, and any created by ingest).
ALTER TABLE environments
    ADD COLUMN base_url   text CHECK (base_url IS NULL OR (base_url ~ '^https?://[^/?#\s]+' AND octet_length(base_url) <= 2048)),
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
