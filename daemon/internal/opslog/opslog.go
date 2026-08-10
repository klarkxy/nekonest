// Package opslog provides the daemon's privacy-safe operational logging.
package opslog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	Format string
	Level  slog.Level
}

func ParseConfig(format, level string) (Config, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return Config{}, fmt.Errorf("invalid NEKONEST_LOG_FORMAT (want text or json)")
	}
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		level = "info"
	}
	levels := map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}
	parsed, ok := levels[level]
	if !ok {
		return Config{}, fmt.Errorf("invalid NEKONEST_LOG_LEVEL (want debug, info, warn or error)")
	}
	return Config{Format: format, Level: parsed}, nil
}

func Configure() (Config, error) {
	cfg, err := ParseConfig(os.Getenv("NEKONEST_LOG_FORMAT"), os.Getenv("NEKONEST_LOG_LEVEL"))
	if err != nil {
		return Config{}, err
	}
	slog.SetDefault(New(os.Stderr, cfg))
	return cfg, nil
}

func New(w io.Writer, cfg Config) *slog.Logger {
	options := &slog.HandlerOptions{Level: cfg.Level}
	if cfg.Format == "json" {
		return slog.New(slog.NewJSONHandler(w, options))
	}
	return slog.New(slog.NewTextHandler(w, options))
}

func emit(level slog.Level, component, event, msg string, args ...any) {
	attrs := make([]any, 0, len(args)+4)
	attrs = append(attrs, "component", component, "event", event)
	attrs = append(attrs, sanitizeAttributes(args)...)
	slog.Default().Log(context.Background(), level, msg, attrs...)
}

func sanitizeAttributes(args []any) []any {
	safe := append([]any(nil), args...)
	for i := 0; i+1 < len(safe); i += 2 {
		key, ok := safe[i].(string)
		if !ok || !strings.HasSuffix(key, "_id") {
			continue
		}
		value, ok := safe[i+1].(string)
		if !ok {
			safe[i+1] = "invalid"
			continue
		}
		safe[i+1] = pseudonymousIdentifier(value)
	}
	return safe
}

func pseudonymousIdentifier(value string) string {
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return "id:" + hex.EncodeToString(digest[:8])
}

func Debug(component, event, msg string, args ...any) {
	emit(slog.LevelDebug, component, event, msg, args...)
}
func Info(component, event, msg string, args ...any) {
	emit(slog.LevelInfo, component, event, msg, args...)
}
func Warn(component, event, msg string, args ...any) {
	emit(slog.LevelWarn, component, event, msg, args...)
}
func Error(component, event, msg string, err error, args ...any) {
	attrs := append([]any{"error", "operation_failed"}, args...)
	emit(slog.LevelError, component, event, msg, attrs...)
}

func RedirectStandard(component string) {
	log.SetFlags(0)
	log.SetPrefix("")
	log.SetOutput(legacyWriter{component: component})
}

type legacyWriter struct{ component string }

func (w legacyWriter) Write(p []byte) (int, error) {
	if len(strings.TrimSpace(string(p))) != 0 {
		Warn(w.component, "legacy_event", "legacy operational event omitted")
	}
	return len(p), nil
}
