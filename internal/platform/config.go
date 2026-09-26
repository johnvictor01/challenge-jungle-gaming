package platform

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL      string
	HTTPAddress      string
	OIDCIssuerURL    string
	OIDCAudience     string
	SQSEndpoint      string
	SQSRegion        string
	SQSQueueURL      string
	SQSInputQueueURL string
	SQSGroupID       string
	WorkerID         string
}

func LoadConfig() (Config, error) {
	config := Config{
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		HTTPAddress:      strings.TrimSpace(os.Getenv("HTTP_ADDR")),
		OIDCIssuerURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL")), "/"),
		OIDCAudience:     strings.TrimSpace(os.Getenv("OIDC_AUDIENCE")),
		SQSEndpoint:      strings.TrimSpace(os.Getenv("SQS_ENDPOINT")),
		SQSRegion:        strings.TrimSpace(os.Getenv("AWS_REGION")),
		SQSQueueURL:      strings.TrimSpace(os.Getenv("SQS_QUEUE_URL")),
		SQSInputQueueURL: strings.TrimSpace(os.Getenv("SQS_INPUT_QUEUE_URL")),
		SQSGroupID:       strings.TrimSpace(os.Getenv("SQS_MESSAGE_GROUP_ID")),
		WorkerID:         strings.TrimSpace(os.Getenv("OUTBOX_WORKER_ID")),
	}
	if config.HTTPAddress == "" {
		config.HTTPAddress = ":8080"
	}
	if config.SQSRegion == "" {
		config.SQSRegion = "us-east-1"
	}
	if config.WorkerID == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "local"
		}
		config.WorkerID = fmt.Sprintf("outbox-%s-%d", hostname, os.Getpid())
	}
	if config.DatabaseURL == "" || config.OIDCIssuerURL == "" || config.OIDCAudience == "" {
		return Config{}, errors.New("DATABASE_URL, OIDC_ISSUER_URL and OIDC_AUDIENCE are required")
	}
	return config, nil
}
