// Package trek provides an authenticated REST client for TREK.
package trek

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

const (
	EnvURL      = "TREK_URL"
	EnvEmail    = "TREK_EMAIL"
	EnvPassword = "TREK_PASSWORD"
)

type Config struct {
	BaseURL  string
	Email    string
	Password string
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv(EnvURL)), "/"),
		Email:    strings.TrimSpace(os.Getenv(EnvEmail)),
		Password: os.Getenv(EnvPassword),
	}

	var missing []string
	if cfg.BaseURL == "" {
		missing = append(missing, EnvURL)
	}
	if cfg.Email == "" {
		missing = append(missing, EnvEmail)
	}
	if cfg.Password == "" {
		missing = append(missing, EnvPassword)
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("required environment variable not set: %s", strings.Join(missing, ", "))
	}

	if err := validateURL(cfg.BaseURL); err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", EnvURL, err)
	}

	return cfg, nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a URL: %w", raw, err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("%s would carry the service-account password and the session token in plain text; only a localhost instance may be reached over http", raw)
		}
	default:
		return fmt.Errorf("%s has no usable scheme; TREK_URL must be an https:// instance address", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("%s names no host", raw)
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("%s carries a path; TREK_URL is the instance root, so the /api paths are appended to it as they stand", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s carries a query or fragment; TREK_URL is bare instance address, so the /api paths are appended to it as they stand", raw)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
