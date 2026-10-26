package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultPort         = "8080"
	DefaultGlobalPrefix = "/api/v2"
	DefaultBackend      = BackendSQLite
	DefaultSQLitePath   = "./data/excalidraw.db"
	DefaultFilesPath    = "./data/blobs"
	DefaultBodyLimit    = 50 << 20 // 50mb
	DefaultLogLevel     = slog.LevelInfo
)

type Backend string

const (
	BackendSQLite Backend = "sqlite"
	BackendFiles  Backend = "files"
)

type Config struct {
	Port         string
	GlobalPrefix string
	Backend      Backend
	SQLitePath   string
	FilesPath    string
	BodyLimit    int64
	LogLevel     slog.Level
}

func FromEnv() (Config, error) {
	cfg := Config{
		Port:         DefaultPort,
		GlobalPrefix: DefaultGlobalPrefix,
		Backend:      DefaultBackend,
		SQLitePath:   DefaultSQLitePath,
		FilesPath:    DefaultFilesPath,
		BodyLimit:    DefaultBodyLimit,
		LogLevel:     DefaultLogLevel,
	}

	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}

	if v, ok := os.LookupEnv("GLOBAL_PREFIX"); ok {
		cfg.GlobalPrefix = NormalizePrefix(v)
	}

	if v := os.Getenv("STORE_BACKEND"); v != "" {
		backend, err := ParseBackend(v)
		if err != nil {
			return Config{}, fmt.Errorf("STORE_BACKEND: %w", err)
		}
		cfg.Backend = backend
	}

	if v := os.Getenv("SQLITE_PATH"); v != "" {
		cfg.SQLitePath = v
	}

	if v := os.Getenv("FILES_PATH"); v != "" {
		cfg.FilesPath = v
	}

	if v := os.Getenv("BODY_LIMIT"); v != "" {
		limit, err := ParseBodyLimit(v)
		if err != nil {
			return Config{}, fmt.Errorf("BODY_LIMIT: %w", err)
		}
		cfg.BodyLimit = limit
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = ParseLogLevel(v)
	}

	return cfg, nil
}

func (c Config) Addr() string {
	return ":" + c.Port
}

// NormalizePrefix sanitizes a global route prefix.
//
//   - "" and "/" become "" (no prefix)
//   - "api/v2" becomes "/api/v2"
//   - "/api/v2/" becomes "/api/v2"
func NormalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	return prefix
}

func ParseBackend(value string) (Backend, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sqlite":
		return BackendSQLite, nil
	case "files":
		return BackendFiles, nil
	default:
		return "", fmt.Errorf("unknown backend %q (want %q or %q)",
			strings.TrimSpace(value), BackendSQLite, BackendFiles)
	}
}

func ParseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug", "verbose":
		return slog.LevelDebug
	case "info", "log":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return DefaultLogLevel
	}
}

func ParseBodyLimit(value string) (int64, error) {
	s := strings.ToLower(strings.TrimSpace(value))
	if s == "" {
		return 0, errors.New("empty size")
	}

	multiplier := int64(1)
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"tb", 1 << 40}, {"gb", 1 << 30}, {"mb", 1 << 20}, {"kb", 1 << 10}, {"b", 1},
		{"t", 1 << 40}, {"g", 1 << 30}, {"m", 1 << 20}, {"k", 1 << 10},
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix.suffix) {
			multiplier = suffix.mult
			s = strings.TrimSpace(strings.TrimSuffix(s, suffix.suffix))
			break
		}
	}

	number, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", value)
	}
	if number <= 0 {
		return 0, fmt.Errorf("size must be positive, got %q", value)
	}

	limit := int64(number * float64(multiplier))
	if limit <= 0 {
		return 0, fmt.Errorf("size %q is too small", value)
	}
	return limit, nil
}
