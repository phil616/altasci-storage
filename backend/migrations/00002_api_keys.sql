-- +goose Up
CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    scopes_json TEXT NOT NULL,
    all_projects INTEGER NOT NULL DEFAULT 0 CHECK (all_projects IN (0, 1)),
    project_ids_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    last_used_at INTEGER,
    revoked_at INTEGER
);
CREATE INDEX api_keys_user_id ON api_keys(user_id);

-- +goose Down
SELECT RAISE(FAIL, 'forward-only migrations: down is intentionally unsupported');
