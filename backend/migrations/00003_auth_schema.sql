-- +goose Up

CREATE EXTENSION IF NOT EXISTS citext;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TABLE auth.users (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email             citext      NOT NULL,
    username          text        NOT NULL,
    password_hash     text,
    email_verified_at timestamptz,
    is_admin          boolean     NOT NULL DEFAULT false,
    deleted_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX auth_users_email_key
    ON auth.users (email)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX auth_users_username_lower_key
    ON auth.users (lower(username))
    WHERE deleted_at IS NULL;

CREATE TRIGGER auth_users_set_updated_at
    BEFORE UPDATE ON auth.users
    FOR EACH ROW
    EXECUTE FUNCTION public.set_updated_at();

CREATE TABLE auth.sessions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   text        NOT NULL UNIQUE,
    user_id      uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    user_agent   text,
    ip_hash      text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);

CREATE INDEX auth_sessions_user_id_idx ON auth.sessions (user_id);
CREATE INDEX auth_sessions_expires_at_idx ON auth.sessions (expires_at);

CREATE TABLE auth.oauth_accounts (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    provider         text        NOT NULL CHECK (provider IN ('google', 'apple', 'steam')),
    provider_user_id text        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX auth_oauth_accounts_provider_key
    ON auth.oauth_accounts (provider, provider_user_id);
CREATE INDEX auth_oauth_accounts_user_id_idx ON auth.oauth_accounts (user_id);

CREATE TABLE auth.verification_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    token_hash text        NOT NULL UNIQUE,
    purpose    text        NOT NULL CHECK (purpose IN ('email_verify', 'password_reset')),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX auth_verification_tokens_user_purpose_idx
    ON auth.verification_tokens (user_id, purpose);
CREATE INDEX auth_verification_tokens_expires_at_idx
    ON auth.verification_tokens (expires_at);

-- +goose Down
DROP TABLE IF EXISTS auth.verification_tokens;
DROP TABLE IF EXISTS auth.oauth_accounts;
DROP TABLE IF EXISTS auth.sessions;
DROP TABLE IF EXISTS auth.users;
DROP FUNCTION IF EXISTS public.set_updated_at();
DROP EXTENSION IF EXISTS citext;
