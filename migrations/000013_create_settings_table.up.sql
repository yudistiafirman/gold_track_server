CREATE TABLE settings (
    id           BIGSERIAL PRIMARY KEY,
    public_id    UUID         NOT NULL DEFAULT gen_random_uuid(),
    key          VARCHAR(100) NOT NULL,
    value        TEXT         NOT NULL,
    description  VARCHAR(300),
    updated_by   BIGINT       NOT NULL REFERENCES users (id),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_settings_key UNIQUE (key),
    CONSTRAINT uq_settings_public_id UNIQUE (public_id)
);
