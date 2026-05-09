package duckdbx

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

type S3Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          string
	URLStyle        string
}

func S3ConfigFromEnv() S3Config {
	return S3Config{
		Endpoint:        strings.TrimSpace(os.Getenv("S3_ENDPOINT")),
		AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		UseSSL:          envDefault("S3_USE_SSL", "false"),
		URLStyle:        envDefault("S3_URL_STYLE", "path"),
	}
}

func ConfigureS3FromEnv(db *sql.DB) error {
	return ConfigureS3(db, S3ConfigFromEnv())
}

func ConfigureS3(db *sql.DB, cfg S3Config) error {
	if db == nil {
		return fmt.Errorf("db is required")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" {
		return fmt.Errorf("S3_ACCESS_KEY_ID is required when S3_ENDPOINT is set")
	}
	if strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return fmt.Errorf("S3_SECRET_ACCESS_KEY is required when S3_ENDPOINT is set")
	}

	statements := []string{
		"INSTALL httpfs",
		"LOAD httpfs",
		"SET s3_endpoint=" + sqlStringLiteral(cfg.Endpoint),
		"SET s3_access_key_id=" + sqlStringLiteral(cfg.AccessKeyID),
		"SET s3_secret_access_key=" + sqlStringLiteral(cfg.SecretAccessKey),
		"SET s3_use_ssl=" + strings.ToLower(strings.TrimSpace(cfg.UseSSL)),
		"SET s3_url_style=" + sqlStringLiteral(cfg.URLStyle),
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("configure duckdb s3: %w", err)
		}
	}
	return nil
}

func envDefault(key string, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func sqlStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
