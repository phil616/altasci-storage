package factory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/altasci/network-storage/backend/internal/storage/local"
	"github.com/altasci/network-storage/backend/internal/storage/oss"
	"github.com/altasci/network-storage/backend/internal/storage/s3"
)

type Factory struct {
	store *repository.Store
	box   *security.SecretBox
}

func New(store *repository.Store, box *security.SecretBox) *Factory {
	return &Factory{store: store, box: box}
}
func (f *Factory) ByID(ctx context.Context, id string) (storageapi.Storage, repository.StorageBackend, error) {
	b, err := f.store.StorageBackendByID(ctx, id)
	if err != nil {
		return nil, b, err
	}
	adapter, err := f.FromBackend(ctx, b)
	return adapter, b, err
}
func (f *Factory) FromBackend(ctx context.Context, b repository.StorageBackend) (storageapi.Storage, error) {
	var secretRaw []byte
	var err error
	if b.SecretCiphertext.Valid {
		secretRaw, err = f.box.Decrypt(b.SecretCiphertext.String, "storage-backend:"+b.ID+":v1")
		if err != nil {
			return nil, fmt.Errorf("decrypt storage credentials: %w", err)
		}
	}
	switch b.Type {
	case "local":
		var cfg struct {
			RootPath string `json:"root_path"`
		}
		if err := json.Unmarshal([]byte(b.ConfigJSON), &cfg); err != nil {
			return nil, err
		}
		return local.New(cfg.RootPath)
	case "s3":
		var cfg s3.Config
		var secret s3.Secret
		if err := json.Unmarshal([]byte(b.ConfigJSON), &cfg); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(secretRaw, &secret); err != nil {
			return nil, err
		}
		return s3.New(ctx, cfg, secret)
	case "aliyun_oss":
		var cfg oss.Config
		var secret oss.Secret
		if err := json.Unmarshal([]byte(b.ConfigJSON), &cfg); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(secretRaw, &secret); err != nil {
			return nil, err
		}
		return oss.New(cfg, secret)
	default:
		return nil, fmt.Errorf("unsupported storage backend type %q", b.Type)
	}
}
