ALTER TABLE template_versions
  ALTER COLUMN external_auth_providers type jsonb
  using to_jsonb(external_auth_providers);
-- so, that will give us ["foo"], but now we need
-- {"github": {optional: false}} or [{id: "github", optional: false}]
