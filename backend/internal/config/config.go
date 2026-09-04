package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server struct {
		Listen string `toml:"listen"`
	} `toml:"server"`
	Database struct {
		Path string `toml:"path"`
	} `toml:"database"`
	Security struct {
		MasterKeyFile string `toml:"master_key_file"`
	} `toml:"security"`
}

func Default() Config {
	cfg := Config{}
	cfg.Server.Listen = "127.0.0.1:8080"
	cfg.Database.Path = "./data/altasci-network-storage.db"
	cfg.Security.MasterKeyFile = "./data/master.key"
	return cfg
}

func Load(path string) (Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if cfg.Server.Listen == "" || cfg.Database.Path == "" || cfg.Security.MasterKeyFile == "" {
		return Config{}, fmt.Errorf("server.listen, database.path and security.master_key_file are required")
	}
	return cfg, nil
}

// LoadOrCreateForInit makes the documented first-run command work from a
// fresh checkout while keeping Load strict for normal server startup.
func LoadOrCreateForInit(path string) (Config, bool, error) {
	cfg, err := Load(path)
	if err == nil {
		return cfg, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, false, err
	}
	cfg = Default()
	if err := EnsureParent(path); err != nil {
		return Config{}, false, fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return Config{}, false, fmt.Errorf("create config: %w", err)
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return Config{}, false, fmt.Errorf("write config: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return Config{}, false, fmt.Errorf("close config: %w", err)
	}
	return cfg, true, nil
}

func EnsureParent(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o750)
}

func ValidatePublicURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.String() != value {
		return fmt.Errorf("must be an exact HTTPS origin without credentials, path, query, fragment, or trailing slash")
	}
	return nil
}
