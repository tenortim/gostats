package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	slogmulti "github.com/samber/slog-multi"
)

const (
	LevelTrace    = slog.Level(-8)
	LevelDebug    = slog.LevelDebug
	LevelInfo     = slog.LevelInfo
	LevelNotice   = slog.Level(2)
	LevelWarning  = slog.LevelWarn
	LevelError    = slog.LevelError
	LevelCritical = slog.Level(10)
	LevelFatal    = slog.Level(12)
)

// Default logger
var log *slog.Logger

// ParseLevel converts a string to a slog.Level.
// It handles standard levels and is case-insensitive.
// If the string does not match a known level, it returns an error.
func ParseLevel(levelStr string) (slog.Level, error) {
	var level slog.Level
	var err error = nil
	switch strings.ToUpper(levelStr) {
	case "TRACE":
		level = LevelTrace
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "NOTICE":
		level = LevelNotice
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	case "CRITICAL":
		level = LevelCritical
	default:
		err = fmt.Errorf("unknown log level '%s'", levelStr)
	}
	return level, err
}

func loggingOptions(level slog.Level) *slog.HandlerOptions {
	return &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize the name of the level key and the output string, including
			// custom level values.
			if a.Key == slog.LevelKey {
				// Handle custom level values.
				level := a.Value.Any().(slog.Level)

				// This could also look up the name from a map or other structure, but
				// this demonstrates using a switch statement to rename levels. For
				// maximum performance, the string values should be constants, but this
				// example uses the raw strings for readability.
				switch {
				case level < LevelDebug:
					a.Value = slog.StringValue("TRACE")
				case level < LevelInfo:
					a.Value = slog.StringValue("DEBUG")
				case level < LevelNotice:
					a.Value = slog.StringValue("INFO")
				case level < LevelWarning:
					a.Value = slog.StringValue("NOTICE")
				case level < LevelError:
					a.Value = slog.StringValue("WARN")
				case level < LevelCritical:
					a.Value = slog.StringValue("ERROR")
				case level < LevelFatal:
					a.Value = slog.StringValue("CRITICAL")
				default:
					a.Value = slog.StringValue("FATAL")
				}
			}

			return a
		},
	}
}

// setupEarlyLogging initializes early logging to stdout at INFO level
// before the full logging configuration is available.
func setupEarlyLogging() {
	// Early logging to stdout at INFO level
	options := loggingOptions(LevelInfo)
	consoleHandler := slog.NewTextHandler(os.Stdout, options)
	log = slog.New(consoleHandler)
}

// setupLogging initializes the logging system based on the global configuration
// and any command-line overrides for the log file name.
func setupLogging(lc loggingConfig, logLevel string, logFileName string) {
	// Determine log level
	// If not set on command line, get from config file
	// If not set in config file, default to NOTICE
	if logLevel == "" {
		if lc.LogLevel == nil {
			logLevel = "NOTICE"
		} else {
			logLevel = *lc.LogLevel
		}
	}
	level, err := ParseLevel(logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gostats: invalid log level '%s' - %s\n", logLevel, err)
		os.Exit(2)
	}

	// Up to two backends (one file, one stdout)
	backends := make([]slog.Handler, 0, 2)
	options := loggingOptions(level)

	// Up to two backends (one file, one stdout)
	// default is to not log to file
	logfile := ""
	// is it set in the config file?
	if lc.LogFile != nil {
		logfile = *lc.LogFile
	}
	// Finally, if it was set on the command line, override the setting
	if logFileName != "" {
		logfile = logFileName
	}
	if logfile != "" {
		f, err := os.OpenFile(logfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gostats: unable to open log file %s for output - %s", logfile, err)
			os.Exit(2)
		}
		var fileHandler slog.Handler
		format := "text"
		if lc.LogFileFormat != nil {
			format = strings.ToLower(*lc.LogFileFormat)
		}
		switch format {
		case "json":
			fileHandler = slog.NewJSONHandler(f, options)
		case "text":
			fileHandler = slog.NewTextHandler(f, options)
		default:
			fmt.Fprintf(os.Stderr, "gostats: unknown log file format '%s'\n", format)
			os.Exit(2)
		}
		backends = append(backends, fileHandler)
	}
	if lc.LogToStdout {
		consoleHandler := slog.NewTextHandler(os.Stdout, options)
		backends = append(backends, consoleHandler)
	}
	if len(backends) == 0 {
		fmt.Fprintf(os.Stderr, "gostats: no logging defined, unable to continue\nPlease configure logging in the config file and/or via the command line\n")
		os.Exit(3)
	}
	log = slog.New(slogmulti.Fanout(backends...))
}
