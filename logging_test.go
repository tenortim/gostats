package main

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
		wantErr  bool
	}{
		{"trace", LevelTrace, false},
		{"TRACE", LevelTrace, false},
		{"debug", slog.LevelDebug, false},
		{"DEBUG", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"INFO", slog.LevelInfo, false},
		{"notice", LevelNotice, false},
		{"NOTICE", LevelNotice, false},
		{"warn", slog.LevelWarn, false},
		{"WARN", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"WARNING", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"ERROR", slog.LevelError, false},
		{"critical", LevelCritical, false},
		{"CRITICAL", LevelCritical, false},
		{"invalid", 0, true},
		{"", 0, true},
		{"fatal", 0, true}, // FATAL is not a valid input level
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level, err := ParseLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseLevel(%q): expected error, got level %v", tt.input, level)
				}
			} else {
				if err != nil {
					t.Errorf("ParseLevel(%q): unexpected error: %v", tt.input, err)
				}
				if level != tt.expected {
					t.Errorf("ParseLevel(%q): expected %v, got %v", tt.input, tt.expected, level)
				}
			}
		})
	}
}

func TestParseLevel_Ordering(t *testing.T) {
	// Verify the relative ordering of custom levels matches expectations
	if LevelTrace >= slog.LevelDebug {
		t.Errorf("expected TRACE < DEBUG, got TRACE=%v DEBUG=%v", LevelTrace, slog.LevelDebug)
	}
	if slog.LevelDebug >= slog.LevelInfo {
		t.Errorf("expected DEBUG < INFO")
	}
	if slog.LevelInfo >= LevelNotice {
		t.Errorf("expected INFO < NOTICE, got INFO=%v NOTICE=%v", slog.LevelInfo, LevelNotice)
	}
	if LevelNotice >= slog.LevelWarn {
		t.Errorf("expected NOTICE < WARN, got NOTICE=%v WARN=%v", LevelNotice, slog.LevelWarn)
	}
	if slog.LevelWarn >= slog.LevelError {
		t.Errorf("expected WARN < ERROR")
	}
	if slog.LevelError >= LevelCritical {
		t.Errorf("expected ERROR < CRITICAL, got ERROR=%v CRITICAL=%v", slog.LevelError, LevelCritical)
	}
	if LevelCritical >= LevelFatal {
		t.Errorf("expected CRITICAL < FATAL, got CRITICAL=%v FATAL=%v", LevelCritical, LevelFatal)
	}
}
