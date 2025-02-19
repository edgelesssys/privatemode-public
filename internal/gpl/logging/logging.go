// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package logging contains utility functions to set up logging for Continuum components.
package logging

import (
	"io"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// Flag is the flag name for setting the logging level.
	Flag = "log-level"
	// FlagShorthand is the shorthand flag name for setting the logging level.
	FlagShorthand = "l"
	// DefaultFlagValue is the default value for the log level flag.
	DefaultFlagValue = "info"
	// DefaultFlagValueCLI is the default value for the log level flag for CLIs.
	// CLIs should use a higher default log level to avoid too much output by default.
	DefaultFlagValueCLI = "warn"
	// FlagInfo is the info string for the log level flag.
	FlagInfo = "set logging level (debug, info, warn, error, or a number)"
)

// NewLogger returns a new [*slog.Logger] at the given log level.
// The logger writes to [os.Stderr] and uses the JSON format.
func NewLogger(logLevel string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: LevelFromString(logLevel, slog.LevelInfo),
	}))
}

// NewCLILogger returns a new [*slog.Logger] at the given log level.
func NewCLILogger(logLevel string, out io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
		Level: LevelFromString(logLevel, slog.LevelWarn),
	}))
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

// NewLogWrapper wraps the given [*slog.Logger] in a [*log.Logger].
// All messages written to the returned [*log.Logger] will be written to the error level of the given [*slog.Logger].
func NewLogWrapper(slogger *slog.Logger) *log.Logger {
	return log.New(loggerWrapper{slogger}, "", 0)
}

// loggerWrapper implements [io.Writer] by writing any data to the error level of the embedded slog logger.
type loggerWrapper struct {
	*slog.Logger
}

// Write implements the [io.Writer] interface by writing the given data to the error level of the embedded slog logger.
func (l loggerWrapper) Write(p []byte) (n int, err error) {
	l.Error(string(p))
	return len(p), nil
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
	return slog.New(slog.NewJSONHandler(io.MultiWriter(writer, output), &slog.HandlerOptions{
		Level: LevelFromString(logLevel, slog.LevelInfo),
	}))
}
