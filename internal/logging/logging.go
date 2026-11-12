// Package logging builds the structured slog logger used by the server and
// CLI. It redacts sensitive attribute names as defense in depth on top of the
// crypto.Secret type.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

var sensitiveKeys = map[string]bool{
	"password":      true,
	"passwd":        true,
	"secret":        true,
	"token":         true,
	"access_token":  true,
	"refresh_token": true,
	"authorization": true,
	"cookie":        true,
	"api_key":       true,
	"apikey":        true,
	"private_key":   true,
	"root_key":      true,
	"value":         true,
}

// New returns a logger writing to w.
func New(w io.Writer, level, format string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: lv,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if sensitiveKeys[strings.ToLower(attr.Key)] {
				return slog.String(attr.Key, "[REDACTED]")
			}
			return attr
		},
	}
	if format == "console" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}
