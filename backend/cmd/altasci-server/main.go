package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/altasci/network-storage/backend/internal/authorization"
	"github.com/altasci/network-storage/backend/internal/config"
	"github.com/altasci/network-storage/backend/internal/database"
	"github.com/altasci/network-storage/backend/internal/httpapi"
	"github.com/altasci/network-storage/backend/internal/oidc"
	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/sharing"
	"github.com/altasci/network-storage/backend/internal/storage/factory"
	"github.com/altasci/network-storage/backend/internal/worker"
	"golang.org/x/term"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			printUsage(os.Stdout)
			return nil
		case "version", "--version":
			fmt.Printf("altasci-server %s\n", version)
			return nil
		}
	}
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "serve":
		return serve(args)
	case "init":
		return initialize(args)
	case "admin-reset-password":
		return resetAdminPassword(args)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, `AltasCI Network Storage backend

Usage:
  altasci-server init --admin-email EMAIL --public-web-url HTTPS_URL --public-api-url HTTPS_URL [--config FILE]
  altasci-server serve [--config FILE]
  altasci-server admin-reset-password [--config FILE]
  altasci-server version

Run "altasci-server <command> --help" for command-specific flags.
The init and password-reset commands must run in an interactive terminal;
password input is intentionally hidden.`)
}

func commonConfig(args []string, name string) (config.Config, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	path := flags.String("config", "config.toml", "bootstrap TOML configuration")
	if err := flags.Parse(args); err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Load(*path)
	if errors.Is(err, os.ErrNotExist) {
		return config.Config{}, fmt.Errorf("bootstrap config %q does not exist; run 'altasci-server init' first: %w", *path, err)
	}
	return cfg, err
}
func openConfigured(cfg config.Config) (*repository.Store, error) {
	if err := config.EnsureParent(cfg.Database.Path); err != nil {
		return nil, err
	}
	db, err := database.Open(cfg.Database.Path)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return repository.New(db), nil
}

func initialize(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := flags.String("config", "config.toml", "bootstrap TOML configuration")
	email := flags.String("admin-email", "", "administrator email")
	webURL := flags.String("public-web-url", "", "public frontend HTTPS URL")
	apiURL := flags.String("public-api-url", "", "public API HTTPS URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *email == "" || *webURL == "" || *apiURL == "" {
		return errors.New("--admin-email, --public-web-url and --public-api-url are required")
	}
	if err := config.ValidatePublicURL(*webURL); err != nil {
		return fmt.Errorf("public web URL: %w", err)
	}
	if err := config.ValidatePublicURL(*apiURL); err != nil {
		return fmt.Errorf("public API URL: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[init] loading bootstrap config %s\n", *configPath)
	cfg, configCreated, err := config.LoadOrCreateForInit(*configPath)
	if err != nil {
		return err
	}
	if configCreated {
		fmt.Fprintf(os.Stderr, "[init] created bootstrap config %s\n", *configPath)
	} else {
		fmt.Fprintf(os.Stderr, "[init] using existing bootstrap config %s\n", *configPath)
	}
	if err := config.EnsureParent(cfg.Database.Path); err != nil {
		return err
	}
	if err := config.EnsureParent(cfg.Security.MasterKeyFile); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "[init] waiting for administrator password; typed characters will not be displayed")
	password, err := readPasswordTwice("Admin password (input hidden): ", "Confirm admin password (input hidden): ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "[init] deriving Argon2id password hash")
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	databaseExisted, err := pathExists(cfg.Database.Path)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[init] opening SQLite database %s and applying migrations\n", cfg.Database.Path)
	store, err := openConfigured(cfg)
	if err != nil {
		if !databaseExisted {
			removeSQLiteFiles(cfg.Database.Path)
		}
		return err
	}
	succeeded := false
	masterKeyCreated := false
	defer func() {
		_ = store.DB.Close()
		if succeeded {
			return
		}
		if !databaseExisted {
			removeSQLiteFiles(cfg.Database.Path)
		}
		if masterKeyCreated {
			_ = os.Remove(cfg.Security.MasterKeyFile)
		}
	}()
	initialized, err := store.IsInitialized(context.Background())
	if err != nil {
		return fmt.Errorf("inspect initialization state: %w", err)
	}
	if initialized {
		return errors.New("database is already initialized; init can only run once")
	}
	masterKeyExists, err := pathExists(cfg.Security.MasterKeyFile)
	if err != nil {
		return err
	}
	if masterKeyExists {
		fmt.Fprintf(os.Stderr, "[init] validating existing master key %s\n", cfg.Security.MasterKeyFile)
		if _, err := security.LoadMasterKey(cfg.Security.MasterKeyFile); err != nil {
			return fmt.Errorf("existing master key is invalid: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "[init] creating master key %s\n", cfg.Security.MasterKeyFile)
		if _, err := security.CreateMasterKey(cfg.Security.MasterKeyFile); err != nil {
			return err
		}
		masterKeyCreated = true
	}
	now := repository.NowMS()
	admin := repository.User{ID: security.NewID(), Email: strings.TrimSpace(*email), PasswordHash: sql.NullString{String: hash, Valid: true}, Role: "admin", WriteEnabled: true, Status: "active", CreatedAt: now, UpdatedAt: now}
	settings := map[string]any{
		"site.public_web_url":           *webURL,
		"site.public_api_url":           *apiURL,
		"cors.allowed_origins":          []string{*webURL},
		"auth.session_idle_timeout":     86400,
		"auth.session_absolute_timeout": 604800,
		"storage.upload_presign_ttl":    900,
		"storage.download_presign_ttl":  300,
		"share.default_expiration":      604800,
		"share.download_presign_ttl":    120,
		"security.trusted_proxy_cidrs":  []string{},
		"security.share_rate_limit":     map[string]int{"ip_attempts": 5, "ip_window_seconds": 60, "ip_ban_seconds": 900, "escalated_ban_seconds": 3600, "share_attempts": 50, "share_window_seconds": 600, "share_ban_seconds": 900},
		"security.login_rate_limit":     map[string]int{"attempts": 10, "window_seconds": 600, "cooldown_seconds": 900},
	}
	fmt.Fprintln(os.Stderr, "[init] atomically creating administrator and system settings")
	if err := store.Initialize(context.Background(), admin, settings); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	succeeded = true
	fmt.Println("AltasCI Network Storage initialized. Back up the database and master key separately.")
	return nil
}

func resetAdminPassword(args []string) error {
	cfg, err := commonConfig(args, "admin-reset-password")
	if err != nil {
		return err
	}
	store, err := openConfigured(cfg)
	if err != nil {
		return err
	}
	defer store.DB.Close()
	users, err := store.ListUsers(context.Background())
	if err != nil {
		return err
	}
	var admin repository.User
	for _, u := range users {
		if u.Role == "admin" && u.Status == "active" {
			admin = u
			break
		}
	}
	if admin.ID == "" {
		return errors.New("active administrator not found")
	}
	password, err := readPasswordTwice("New admin password: ", "Confirm new admin password: ")
	if err != nil {
		return err
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	if err := store.SetPassword(context.Background(), admin.ID, hash); err != nil {
		return err
	}
	fmt.Println("Administrator password reset; all administrator sessions were revoked.")
	return nil
}

func serve(args []string) error {
	fmt.Fprintln(os.Stderr, "[serve] loading configuration and checking initialization state")
	cfg, err := commonConfig(args, "serve")
	if err != nil {
		return err
	}
	databaseExists, err := pathExists(cfg.Database.Path)
	if err != nil {
		return err
	}
	if !databaseExists {
		return fmt.Errorf("database %q does not exist; run 'altasci-server init' first", cfg.Database.Path)
	}
	key, err := security.LoadMasterKey(cfg.Security.MasterKeyFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("master key %q does not exist; run 'altasci-server init' first", cfg.Security.MasterKeyFile)
		}
		return err
	}
	store, err := openConfigured(cfg)
	if err != nil {
		return err
	}
	defer store.DB.Close()
	initialized, err := store.IsInitialized(context.Background())
	if err != nil {
		return fmt.Errorf("inspect initialization state: %w", err)
	}
	if !initialized {
		return errors.New("database initialization is incomplete; run 'altasci-server init' to recover it")
	}
	fmt.Fprintln(os.Stderr, "[serve] initialization verified; starting worker and HTTP server")
	box, err := security.NewSecretBox(key)
	if err != nil {
		return err
	}
	var webURL, apiURL string
	if err := store.Setting(context.Background(), "site.public_web_url", &webURL); err != nil {
		return fmt.Errorf("public web URL is not initialized: %w", err)
	}
	if err := store.Setting(context.Background(), "site.public_api_url", &apiURL); err != nil {
		return fmt.Errorf("public API URL is not initialized: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	storageFactory := factory.New(store, box)
	oidcService := oidc.New(store, box, apiURL)
	handler := httpapi.New(httpapi.Options{Store: store, Authorization: authorization.New(store), Factory: storageFactory, Box: box, Sharing: sharing.New(key), OIDC: oidcService, Log: log, WebURL: webURL, APIURL: apiURL})
	server := &http.Server{Addr: cfg.Server.Listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go worker.New(store, storageFactory, log).Run(ctx)
	errCh := make(chan error, 1)
	go func() { log.Info("server listening", "address", cfg.Server.Listen); errCh <- server.ListenAndServe() }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect %q: %w", path, err)
}

func removeSQLiteFiles(path string) {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Remove(candidate)
	}
}

func readPasswordTwice(first, second string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("password must be read from an interactive TTY")
	}
	fmt.Fprint(os.Stderr, first)
	one, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, second)
	two, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(one) != string(two) {
		return "", errors.New("passwords do not match")
	}
	return string(one), nil
}
