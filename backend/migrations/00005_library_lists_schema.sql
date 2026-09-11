-- +goose Up

CREATE TABLE library.user_lists (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
    name            text        NOT NULL,
    kind            text        NOT NULL DEFAULT 'custom' CHECK (kind IN ('custom', 'wishlist')),
    description     text,
    visibility      text        NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'unlisted', 'private')),
    cover_image_url text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX library_user_lists_user_id_idx ON library.user_lists (user_id);
CREATE INDEX library_user_lists_visibility_idx ON library.user_lists (visibility);

CREATE UNIQUE INDEX library_user_lists_one_wishlist_per_user_key
    ON library.user_lists (user_id)
    WHERE kind = 'wishlist';

CREATE TRIGGER library_user_lists_set_updated_at
    BEFORE UPDATE ON library.user_lists
    FOR EACH ROW
    EXECUTE FUNCTION public.set_updated_at();

CREATE TABLE library.user_list_items (
    id       uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    list_id  uuid        NOT NULL REFERENCES library.user_lists (id) ON DELETE CASCADE,
    game_id  bigint      NOT NULL REFERENCES catalog.games (id) ON DELETE RESTRICT,
    position integer     NOT NULL DEFAULT 0,
    note     text,
    added_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX library_user_list_items_list_game_key
    ON library.user_list_items (list_id, game_id);
CREATE INDEX library_user_list_items_list_position_idx
    ON library.user_list_items (list_id, position);

-- +goose Down
DROP TABLE IF EXISTS library.user_list_items;
DROP TABLE IF EXISTS library.user_lists;
