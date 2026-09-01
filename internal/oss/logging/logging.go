// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package logging contains utility functions to set up logging for Continuum components.
package logging

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/requestid"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// Flag is the flag name for setting the logging level.
	Flag = "log-level"
	// FormatFlag is the flag name for setting the logging format.
	FormatFlag = "log-format"
	// FlagShorthand is the shorthand flag name for setting the logging level.
	FlagShorthand = "l"
	// DefaultFlagValue is the default value for the log level flag.
	DefaultFlagValue = "info"
	// DefaultFlagValueCLI is the default value for the log level flag for CLIs.
	// CLIs should use a higher default log level to avoid too much output by default.
	DefaultFlagValueCLI = "warn"
	// DefaultFormatFlagValue is the default value for the log format flag.
	DefaultFormatFlagValue = FormatFlagValueText
	// FormatFlagValueJSON is the format flag value for JSON logging.
	FormatFlagValueJSON = "json"
	// FormatFlagValueText is the format flag value for standard text logging.
	FormatFlagValueText = "text"
	// FlagInfo is the info string for the log level flag.
	FlagInfo = "set logging level (debug, info, warn, error, or a number)"
	// FormatFlagInfo is the info string for the log format flag.
	FormatFlagInfo = "set logging format (json or text)"
)

// RegisterFlagCompletionFunc registers a completion function for the log level flag.
func RegisterFlagCompletionFunc(cmd *cobra.Command) error {
	return cmd.RegisterFlagCompletionFunc(Flag, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp
	})
}

// RegisterFormatFlagCompletionFunc registers a completion function for the format flag.
func RegisterFormatFlagCompletionFunc(cmd *cobra.Command) error {
	return cmd.RegisterFlagCompletionFunc(FormatFlag, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{FormatFlagValueJSON, FormatFlagValueText}, cobra.ShellCompDirectiveNoFileComp
	})
}

// ValidateLogFormat validates the log format.
func ValidateLogFormat(logFormat string) error {
	switch strings.ToLower(logFormat) {
	case FormatFlagValueJSON, FormatFlagValueText:
		return nil
	default:
		return fmt.Errorf("invalid log format %q: --%s must be one of %q", logFormat, FormatFlag, []string{FormatFlagValueJSON, FormatFlagValueText})
	}
}

// contextHandler wraps a [slog.Handler] and enriches each record with attributes extracted from the
// context at log time (if context is passed in the logging call e.g. [slog.Logger.InfoContext]).
// Attributes added: request ID via [requestid.FromContext].
type contextHandler struct {
	handler slog.Handler
}

var _ slog.Handler = (*contextHandler)(nil)

func newContextHandler(handler slog.Handler) *contextHandler {
	return &contextHandler{handler}
}

func (c contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return c.handler.Enabled(ctx, level)
}

func (c contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := requestid.FromContext(ctx); ok {
		r.AddAttrs(slog.String("requestID", id))
	}
	return c.handler.Handle(ctx, r)
}

func (c contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return newContextHandler(c.handler.WithAttrs(attrs))
}

func (c contextHandler) WithGroup(name string) slog.Handler {
	return newContextHandler(c.handler.WithGroup(name))
}

// NewLogger returns a new [*slog.Logger] at the given log level.
// The logger writes to [os.Stderr] and uses the JSON format.
func NewLogger(logLevel string) *slog.Logger {
	baseHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: LevelFromString(logLevel, slog.LevelInfo),
	})
	return slog.New(newContextHandler(baseHandler))
}

// NewCLILogger returns a new [*slog.Logger] at the given log level.
func NewCLILogger(logLevel string, out io.Writer) *slog.Logger {
	baseHandler := slog.NewTextHandler(out, &slog.HandlerOptions{
		Level: LevelFromString(logLevel, slog.LevelWarn),
	})
	return slog.New(newContextHandler(baseHandler))
}

// LevelFromString converts a string to a [slog.Level].
// If the given string cannot be translated to a [slog.Level], or is not a number,
// the given fallback is used instead.
//
// This is a low level function
// Unless setting up the logger manually is required, use [NewLogger] instead.
func LevelFromString(s string, fallback slog.Level) slog.Level {
	var level slog.Level
	switch strings.ToLower(s) {
	case "debug":
		level = slog.LevelDebug
	case "":
		fallthrough
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		numericLevel, err := strconv.Atoi(s)
		if err != nil {
			numericLevel = int(fallback)
		}
		level = slog.Level(numericLevel)
	}

	return level
}

// NewFileLogger returns a new [*slog.Logger] that writes to a file with rotation support.
func NewFileLogger(logLevel string, output io.Writer, filename string) *slog.Logger {
	writer := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    100,   // megabytes
		MaxBackups: 2,     // once maxSize is reached, the (backup)file is renamed with a timestamp and a new file is created
		MaxAge:     14,    // days
		Compress:   false, // compress old files
		LocalTime:  false,
	}
	baseHandler := slog.NewJSONHandler(io.MultiWriter(writer, output), &slog.HandlerOptions{
		Level: LevelFromString(logLevel, slog.LevelInfo),
	})
	return slog.New(newContextHandler(baseHandler))
}

// NewHTTPErrorLogger returns a [*log.Logger] suitable for usage as an error logger for http servers.
// The logger filters out TLS handshake errors, which are common for servers exposed on public endpoints,
// or TLS endpoints that are scraped by Prometheus.
//
// The logger sets the attribute "source" to "httpErrorLog".
// Additional attributes may be provided to add to the logger, e.g. "component", "usageCollector".
func NewHTTPErrorLogger(slogger *slog.Logger, args ...any) *log.Logger {
	slogger = slogger.With(append([]any{"source", "httpErrorLog"}, args...)...)

	return log.New(&httpLogger{
		infoLog:  slog.NewLogLogger(slogger.Handler(), slog.LevelInfo),
		errorLog: slog.NewLogLogger(slogger.Handler(), slog.LevelError),
	}, "", 0)
}

type httpLogger struct {
	infoLog  *log.Logger
	errorLog *log.Logger
}

func (h *httpLogger) Write(b []byte) (n int, err error) {
	s := string(b)
	if strings.HasPrefix(s, "http: TLS handshake error from") {
		h.infoLog.Print(s)
	} else {
		h.errorLog.Print(s)
	}
	return len(b), nil
}
