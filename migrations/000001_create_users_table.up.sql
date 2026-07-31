CREATE TABLE users (
    id             BIGSERIAL PRIMARY KEY,
    public_id      UUID         NOT NULL DEFAULT gen_random_uuid(),
    name           VARCHAR(200) NOT NULL,
    email          VARCHAR(200) NOT NULL,
    password_hash  VARCHAR(255) NOT NULL,
    role           VARCHAR(20)  NOT NULL CHECK (role IN ('SUPER_ADMIN', 'ADMIN', 'KASIR')),
    is_active      BOOLEAN      NOT NULL DEFAULT true,
    last_login_at  TIMESTAMPTZ,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_users_email UNIQUE (email),
    CONSTRAINT uq_users_public_id UNIQUE (public_id)
);
