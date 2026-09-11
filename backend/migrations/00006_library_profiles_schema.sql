-- +goose Up

CREATE TABLE library.profiles (
    user_id        uuid        PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE,
    display_name   text,
    bio            text,
    avatar_url     text,
    is_public      boolean     NOT NULL DEFAULT true,
    notes_public   boolean     NOT NULL DEFAULT true,
    ratings_public boolean     NOT NULL DEFAULT true,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER library_profiles_set_updated_at
    BEFORE UPDATE ON library.profiles
    FOR EACH ROW
    EXECUTE FUNCTION public.set_updated_at();

-- +goose Down
DROP TABLE IF EXISTS library.profiles;
