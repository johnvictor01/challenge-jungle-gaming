package platform

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL   string
	HTTPAddress   string
	OIDCIssuerURL string
	OIDCAudience  string
}

func LoadConfig() (Config, error) {
	config := Config{
		DatabaseURL:   strings.TrimSpace(os.Getenv("DATABASE_URL")),
		HTTPAddress:   strings.TrimSpace(os.Getenv("HTTP_ADDR")),
		OIDCIssuerURL: strings.TrimRight(strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL")), "/"),
		OIDCAudience:  strings.TrimSpace(os.Getenv("OIDC_AUDIENCE")),
	}
	if config.HTTPAddress == "" {
		config.HTTPAddress = ":8080"
	}
	if config.DatabaseURL == "" || config.OIDCIssuerURL == "" || config.OIDCAudience == "" {
		return Config{}, errors.New("DATABASE_URL, OIDC_ISSUER_URL and OIDC_AUDIENCE are required")
	}
	return config, nil
}
