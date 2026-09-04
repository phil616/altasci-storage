package repository

import (
	"database/sql"
	"errors"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
)

type User struct {
	ID              string         `json:"id"`
	Email           string         `json:"email"`
	EmailNormalized string         `json:"-"`
	PasswordHash    sql.NullString `json:"-"`
	Role            string         `json:"role"`
	WriteEnabled    bool           `json:"write_enabled"`
	Status          string         `json:"status"`
	CreatedAt       int64          `json:"created_at"`
	UpdatedAt       int64          `json:"updated_at"`
	LastLoginAt     sql.NullInt64  `json:"last_login_at"`
}

type Session struct {
	ID                string
	UserID            string
	TokenHash         string
	CSRFTokenHash     string
	CreatedAt         int64
	LastSeenAt        int64
	IdleExpiresAt     int64
	AbsoluteExpiresAt int64
	IP                string
	UserAgent         string
	RevokedAt         sql.NullInt64
}

type StorageBackend struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Type             string         `json:"type"`
	Enabled          bool           `json:"enabled"`
	ConfigJSON       string         `json:"config_json"`
	SecretCiphertext sql.NullString `json:"-"`
	CreatedAt        int64          `json:"created_at"`
	UpdatedAt        int64          `json:"updated_at"`
	LastTestAt       sql.NullInt64  `json:"last_test_at"`
	LastTestStatus   sql.NullString `json:"last_test_status"`
	LastTestMessage  sql.NullString `json:"last_test_message"`
	ProjectCount     int            `json:"project_count"`
	BlobCount        int            `json:"blob_count"`
}

type Project struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	StorageBackendID string `json:"storage_backend_id"`
	CreatedBy        string `json:"created_by"`
	Status           string `json:"status"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
	Permission       string `json:"permission,omitempty"`
}

type ProjectMember struct {
	ProjectID  string `json:"project_id"`
	UserID     string `json:"user_id"`
	Permission string `json:"permission"`
	GrantedBy  string `json:"granted_by"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type Node struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"project_id"`
	ParentID       sql.NullString `json:"parent_id"`
	NodeType       string         `json:"node_type"`
	Name           string         `json:"name"`
	NormalizedName string         `json:"-"`
	CurrentBlobID  sql.NullString `json:"current_blob_id"`
	Size           int64          `json:"size"`
	MIMEType       string         `json:"mime_type"`
	CreatedBy      string         `json:"created_by"`
	CreatedAt      int64          `json:"created_at"`
	UpdatedAt      int64          `json:"updated_at"`
	DeletedAt      sql.NullInt64  `json:"deleted_at"`
}

type Blob struct {
	ID                string         `json:"id"`
	ProjectID         string         `json:"project_id"`
	StorageBackendID  string         `json:"storage_backend_id"`
	ObjectKey         string         `json:"-"`
	Size              int64          `json:"size"`
	MIMEType          string         `json:"mime_type"`
	ETag              sql.NullString `json:"etag"`
	ChecksumAlgorithm sql.NullString `json:"checksum_algorithm"`
	ChecksumValue     sql.NullString `json:"checksum_value"`
	Status            string         `json:"status"`
	CreatedAt         int64          `json:"created_at"`
	DeletedAt         sql.NullInt64  `json:"deleted_at"`
}

type Upload struct {
	ID               string         `json:"id"`
	ProjectID        string         `json:"project_id"`
	NodeID           sql.NullString `json:"node_id"`
	BlobID           string         `json:"blob_id"`
	UserID           string         `json:"user_id"`
	UploadType       string         `json:"upload_type"`
	ProviderUploadID sql.NullString `json:"-"`
	ExpectedSize     int64          `json:"expected_size"`
	MIMEType         string         `json:"mime_type"`
	OriginalName     string         `json:"original_name"`
	ParentID         sql.NullString `json:"parent_id"`
	Overwrite        bool           `json:"overwrite"`
	Status           string         `json:"status"`
	CreatedAt        int64          `json:"created_at"`
	ExpiresAt        int64          `json:"expires_at"`
	CompletedAt      sql.NullInt64  `json:"completed_at"`
}

type Share struct {
	ID              string         `json:"id"`
	ProjectID       string         `json:"project_id"`
	TargetNodeID    string         `json:"target_node_id"`
	CreatedBy       string         `json:"created_by"`
	PublicTokenHash string         `json:"-"`
	CodeHash        sql.NullString `json:"-"`
	RequireCode     bool           `json:"require_code"`
	CodeLength      int            `json:"code_length"`
	ExpiresAt       sql.NullInt64  `json:"expires_at"`
	DisabledAt      sql.NullInt64  `json:"disabled_at"`
	CreatedAt       int64          `json:"created_at"`
}

type OIDCProvider struct {
	ID                     string         `json:"id"`
	Name                   string         `json:"name"`
	Issuer                 string         `json:"issuer"`
	ClientID               string         `json:"client_id"`
	ClientSecretCiphertext sql.NullString `json:"-"`
	Scopes                 string         `json:"scopes"`
	Enabled                bool           `json:"enabled"`
	AutoCreateUser         bool           `json:"auto_create_user"`
	AutoLinkVerifiedEmail  bool           `json:"auto_link_verified_email"`
	AllowedEmailDomains    string         `json:"allowed_email_domains"`
	CreatedAt              int64          `json:"created_at"`
	UpdatedAt              int64          `json:"updated_at"`
}
