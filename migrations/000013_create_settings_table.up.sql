CREATE TABLE settings (
    id           BIGSERIAL PRIMARY KEY,
    key          VARCHAR(100) NOT NULL,
    value        TEXT         NOT NULL,
    description  VARCHAR(300),
    updated_by   BIGINT       NOT NULL REFERENCES users (id),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_settings_key UNIQUE (key)
);
