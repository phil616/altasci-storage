package httpapi

import (
	"context"
	"time"
)

// durationSetting stores durations as JSON seconds so changes made in the Admin
// UI can take effect without restarting the API.
func (s *Server) durationSetting(ctx context.Context, key string, fallback, minimum, maximum time.Duration) time.Duration {
	var seconds float64
	if err := s.store.Setting(ctx, key, &seconds); err != nil {
		return fallback
	}
	value := time.Duration(seconds * float64(time.Second))
	if value < minimum || value > maximum {
		return fallback
	}
	return value
}

type loginRateLimit struct {
	Attempts        int `json:"attempts"`
	WindowSeconds   int `json:"window_seconds"`
	CooldownSeconds int `json:"cooldown_seconds"`
}

func (s *Server) loginRateLimit(ctx context.Context) loginRateLimit {
	value := loginRateLimit{Attempts: 10, WindowSeconds: 600, CooldownSeconds: 900}
	var configured loginRateLimit
	if s.store.Setting(ctx, "security.login_rate_limit", &configured) == nil && configured.Attempts >= 1 && configured.Attempts <= 100 && configured.WindowSeconds >= 10 && configured.WindowSeconds <= 86400 && configured.CooldownSeconds >= 10 && configured.CooldownSeconds <= 86400 {
		return configured
	}
	return value
}

type shareRateLimit struct {
	IPAttempts          int `json:"ip_attempts"`
	IPWindowSeconds     int `json:"ip_window_seconds"`
	IPBanSeconds        int `json:"ip_ban_seconds"`
	EscalatedBanSeconds int `json:"escalated_ban_seconds"`
	ShareAttempts       int `json:"share_attempts"`
	ShareWindowSeconds  int `json:"share_window_seconds"`
	ShareBanSeconds     int `json:"share_ban_seconds"`
}

func (s *Server) shareRateLimit(ctx context.Context) shareRateLimit {
	value := shareRateLimit{IPAttempts: 5, IPWindowSeconds: 60, IPBanSeconds: 900, EscalatedBanSeconds: 3600, ShareAttempts: 50, ShareWindowSeconds: 600, ShareBanSeconds: 900}
	var configured shareRateLimit
	if s.store.Setting(ctx, "security.share_rate_limit", &configured) == nil && configured.IPAttempts >= 1 && configured.IPWindowSeconds >= 10 && configured.IPBanSeconds >= 10 && configured.EscalatedBanSeconds >= configured.IPBanSeconds && configured.ShareAttempts >= 1 && configured.ShareWindowSeconds >= 10 && configured.ShareBanSeconds >= 10 {
		return configured
	}
	return value
}
