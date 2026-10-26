package config

import (
	"log/slog"
	"os"
	"testing"
)

func TestNormalizePrefix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/", ""},
		{"/api/v2", "/api/v2"},
		{"/api/v2/", "/api/v2"},
		{"api/v2", "/api/v2"},
		{"  /api/v2  ", "/api/v2"},
		{"//api", "//api"},
	}
	for _, tt := range tests {
		if got := NormalizePrefix(tt.in); got != tt.want {
			t.Errorf("NormalizePrefix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseBodyLimit(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"50mb", 50 << 20, false},
		{"50MB", 50 << 20, false},
		{"1kb", 1 << 10, false},
		{"512", 512, false},
		{"512b", 512, false},
		{"1.5mb", 1572864, false},
		{"2gb", 2 << 30, false},
		{"1k", 1 << 10, false},
		{"", 0, true},
		{"abc", 0, true},
		{"-1mb", 0, true},
		{"0", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseBodyLimit(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseBodyLimit(%q) = %d, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseBodyLimit(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseBodyLimit(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseBackend(t *testing.T) {
	tests := []struct {
		in      string
		want    Backend
		wantErr bool
	}{
		{"sqlite", BackendSQLite, false},
		{"SQLite", BackendSQLite, false},
		{" files ", BackendFiles, false},
		{"FILES", BackendFiles, false},
		{"db", "", true},
		{"file", "", true},
		{"fs", "", true},
		{"memory", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := ParseBackend(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseBackend(%q) = %q, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseBackend(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseBackend(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"verbose", slog.LevelDebug},
		{"log", slog.LevelInfo},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"nonsense", DefaultLogLevel},
	}
	for _, tt := range tests {
		if got := ParseLogLevel(tt.in); got != tt.want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFromEnvDefaults(t *testing.T) {
	unsetenv(t, "PORT")
	unsetenv(t, "GLOBAL_PREFIX")
	unsetenv(t, "STORE_BACKEND")
	unsetenv(t, "SQLITE_PATH")
	unsetenv(t, "FILES_PATH")
	unsetenv(t, "BODY_LIMIT")
	unsetenv(t, "LOG_LEVEL")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %q, want %q", cfg.Port, DefaultPort)
	}
	if cfg.GlobalPrefix != DefaultGlobalPrefix {
		t.Errorf("GlobalPrefix = %q, want %q", cfg.GlobalPrefix, DefaultGlobalPrefix)
	}
	if cfg.Backend != DefaultBackend {
		t.Errorf("Backend = %q, want %q", cfg.Backend, DefaultBackend)
	}
	if cfg.SQLitePath != DefaultSQLitePath {
		t.Errorf("SQLitePath = %q, want %q", cfg.SQLitePath, DefaultSQLitePath)
	}
	if cfg.FilesPath != DefaultFilesPath {
		t.Errorf("FilesPath = %q, want %q", cfg.FilesPath, DefaultFilesPath)
	}
	if cfg.BodyLimit != DefaultBodyLimit {
		t.Errorf("BodyLimit = %d, want %d", cfg.BodyLimit, DefaultBodyLimit)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.Addr() != ":8080" {
		t.Errorf("Addr() = %q, want %q", cfg.Addr(), ":8080")
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("GLOBAL_PREFIX", "custom/v1/")
	t.Setenv("STORE_BACKEND", "files")
	t.Setenv("SQLITE_PATH", "/tmp/excalidraw.db")
	t.Setenv("FILES_PATH", "/tmp/blobs")
	t.Setenv("BODY_LIMIT", "10mb")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error: %v", err)
	}
	if cfg.Addr() != ":9090" {
		t.Errorf("Addr() = %q, want %q", cfg.Addr(), ":9090")
	}
	if cfg.GlobalPrefix != "/custom/v1" {
		t.Errorf("GlobalPrefix = %q, want %q", cfg.GlobalPrefix, "/custom/v1")
	}
	if cfg.Backend != BackendFiles {
		t.Errorf("Backend = %q, want %q", cfg.Backend, BackendFiles)
	}
	if cfg.SQLitePath != "/tmp/excalidraw.db" {
		t.Errorf("SQLitePath = %q", cfg.SQLitePath)
	}
	if cfg.FilesPath != "/tmp/blobs" {
		t.Errorf("FilesPath = %q", cfg.FilesPath)
	}
	if cfg.BodyLimit != 10<<20 {
		t.Errorf("BodyLimit = %d, want %d", cfg.BodyLimit, 10<<20)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
}

func TestFromEnvEmptyPrefixDisablesIt(t *testing.T) {
	t.Setenv("GLOBAL_PREFIX", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error: %v", err)
	}
	if cfg.GlobalPrefix != "" {
		t.Errorf("GlobalPrefix = %q, want empty", cfg.GlobalPrefix)
	}
}

func TestFromEnvInvalidBackend(t *testing.T) {
	t.Setenv("STORE_BACKEND", "redis")

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() = nil error, want error for an unknown backend")
	}
}

func TestFromEnvInvalidBodyLimit(t *testing.T) {
	t.Setenv("BODY_LIMIT", "not a size")

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() = nil error, want error")
	}
}

func unsetenv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, value)
		}
	})
}
