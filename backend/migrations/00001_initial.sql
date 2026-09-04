-- +goose Up
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    email_normalized TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    write_enabled INTEGER NOT NULL DEFAULT 0 CHECK (write_enabled IN (0, 1)),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    last_login_at INTEGER
);
CREATE UNIQUE INDEX users_one_active_admin ON users(role) WHERE role = 'admin' AND status = 'active';

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    token_hash TEXT NOT NULL UNIQUE,
    csrf_token_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    idle_expires_at INTEGER NOT NULL,
    absolute_expires_at INTEGER NOT NULL,
    ip TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    revoked_at INTEGER
);
CREATE INDEX sessions_user_id ON sessions(user_id);

CREATE TABLE storage_backends (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL CHECK (type IN ('local', 's3', 'aliyun_oss')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    config_json TEXT NOT NULL,
    secret_ciphertext TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    last_test_at INTEGER,
    last_test_status TEXT,
    last_test_message TEXT
);

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    storage_backend_id TEXT NOT NULL REFERENCES storage_backends(id),
    created_by TEXT NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleting', 'delete_failed')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX projects_storage_backend_id ON projects(storage_backend_id);

CREATE TABLE project_members (
    project_id TEXT NOT NULL REFERENCES projects(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    permission TEXT NOT NULL CHECK (permission IN ('read', 'write')),
    granted_by TEXT NOT NULL REFERENCES users(id),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_members_user_id ON project_members(user_id);
CREATE INDEX project_members_project_id ON project_members(project_id);

CREATE TABLE file_blobs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    storage_backend_id TEXT NOT NULL REFERENCES storage_backends(id),
    object_key TEXT NOT NULL UNIQUE,
    size INTEGER NOT NULL CHECK (size >= 0),
    mime_type TEXT NOT NULL,
    etag TEXT,
    checksum_algorithm TEXT,
    checksum_value TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'deleting', 'deleted', 'failed')),
    created_at INTEGER NOT NULL,
    deleted_at INTEGER
);
CREATE INDEX file_blobs_project_id ON file_blobs(project_id);
CREATE INDEX file_blobs_storage_backend_id ON file_blobs(storage_backend_id);
CREATE INDEX file_blobs_status ON file_blobs(status);

CREATE TABLE fs_nodes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    parent_id TEXT REFERENCES fs_nodes(id),
    node_type TEXT NOT NULL CHECK (node_type IN ('file', 'directory')),
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    current_blob_id TEXT REFERENCES file_blobs(id),
    size INTEGER NOT NULL DEFAULT 0 CHECK (size >= 0),
    mime_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    created_by TEXT NOT NULL REFERENCES users(id),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER,
	CHECK ((node_type = 'directory' AND current_blob_id IS NULL) OR (node_type = 'file' AND current_blob_id IS NOT NULL))
);
CREATE INDEX fs_nodes_project_parent ON fs_nodes(project_id, parent_id);
CREATE INDEX fs_nodes_project_name ON fs_nodes(project_id, name);
CREATE INDEX fs_nodes_current_blob ON fs_nodes(current_blob_id);
CREATE UNIQUE INDEX fs_nodes_unique_live_name
    ON fs_nodes(project_id, COALESCE(parent_id, ''), normalized_name)
    WHERE deleted_at IS NULL;

CREATE TABLE upload_sessions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    node_id TEXT REFERENCES fs_nodes(id),
    blob_id TEXT NOT NULL REFERENCES file_blobs(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    upload_type TEXT NOT NULL CHECK (upload_type IN ('single', 'multipart', 'local')),
    provider_upload_id TEXT,
    expected_size INTEGER NOT NULL CHECK (expected_size >= 0),
    mime_type TEXT NOT NULL,
    original_name TEXT NOT NULL,
    parent_id TEXT REFERENCES fs_nodes(id),
    overwrite INTEGER NOT NULL DEFAULT 0 CHECK (overwrite IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('created', 'uploading', 'completing', 'completed', 'aborted', 'expired', 'failed')),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    completed_at INTEGER
);
CREATE INDEX upload_sessions_user_id ON upload_sessions(user_id);
CREATE INDEX upload_sessions_status_expires ON upload_sessions(status, expires_at);

CREATE TABLE shares (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    target_node_id TEXT NOT NULL REFERENCES fs_nodes(id),
    created_by TEXT NOT NULL REFERENCES users(id),
    public_token_hash TEXT NOT NULL UNIQUE,
    code_hash TEXT,
    code_length INTEGER NOT NULL DEFAULT 8 CHECK (code_length IN (4, 8)),
    require_code INTEGER NOT NULL DEFAULT 1 CHECK (require_code IN (0, 1)),
    expires_at INTEGER,
    disabled_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX shares_target_node_id ON shares(target_node_id);
CREATE INDEX shares_expires_at ON shares(expires_at);

CREATE TABLE security_bans (
    id TEXT PRIMARY KEY,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('share_ip', 'share', 'login')),
    scope_key TEXT NOT NULL,
    failure_count INTEGER NOT NULL DEFAULT 0,
    window_started_at INTEGER NOT NULL,
    ban_count INTEGER NOT NULL DEFAULT 0,
    ban_history_started_at INTEGER NOT NULL,
    banned_until INTEGER,
    updated_at INTEGER NOT NULL,
    UNIQUE(scope_type, scope_key)
);

CREATE TABLE background_jobs (
    id TEXT PRIMARY KEY,
    job_type TEXT NOT NULL CHECK (job_type IN ('delete_blob', 'cleanup_upload', 'purge_tombstone')),
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'done', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_run_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    last_error TEXT
);
CREATE INDEX background_jobs_status_next_run ON background_jobs(status, next_run_at);

CREATE TABLE audit_logs (
    id TEXT PRIMARY KEY,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'public', 'system')),
    actor_user_id TEXT REFERENCES users(id),
    action TEXT NOT NULL,
    project_id TEXT REFERENCES projects(id),
    target_id TEXT,
    client_ip TEXT,
    request_id TEXT,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);
CREATE INDEX audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX audit_logs_actor_user_id ON audit_logs(actor_user_id);

CREATE TABLE system_settings (
    key TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_by TEXT REFERENCES users(id),
    updated_at INTEGER NOT NULL
);

CREATE TABLE oidc_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_ciphertext TEXT,
    scopes TEXT NOT NULL DEFAULT 'openid email profile',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    auto_create_user INTEGER NOT NULL DEFAULT 0 CHECK (auto_create_user IN (0, 1)),
    auto_link_verified_email INTEGER NOT NULL DEFAULT 0 CHECK (auto_link_verified_email IN (0, 1)),
    allowed_email_domains TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE oidc_auth_flows (
    state_hash TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES oidc_providers(id),
    pkce_verifier_ciphertext TEXT NOT NULL,
    nonce TEXT NOT NULL,
    return_to TEXT NOT NULL,
    remember_session INTEGER NOT NULL DEFAULT 0 CHECK (remember_session IN (0, 1)),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE external_identities (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES oidc_providers(id),
    subject TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id),
    email_at_link TEXT,
    created_at INTEGER NOT NULL,
    last_login_at INTEGER NOT NULL,
    UNIQUE(provider_id, subject)
);

-- +goose Down
SELECT RAISE(FAIL, 'forward-only migrations: down is intentionally unsupported');
