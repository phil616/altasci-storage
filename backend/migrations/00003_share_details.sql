-- +goose Up
ALTER TABLE shares ADD COLUMN credentials_ciphertext TEXT;
ALTER TABLE shares ADD COLUMN target_node_ids_json TEXT NOT NULL DEFAULT '[]';

-- +goose Down
SELECT RAISE(FAIL, 'forward-only migrations: down is intentionally unsupported');
