package logging

import (
	"log/slog"
	"testing"
)

func TestLevelNames(t *testing.T) {
	names := LevelNames()
	want := []string{"DEBUG", "INFO", "NOTICE", "WARNING", "ERROR", "EMERGENCY"}
	if len(names) != len(want) {
		t.Fatalf("LevelNames() len = %d, want %d", len(names), len(want))
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("LevelNames()[%d] = %q, want %q", i, n, want[i])
		}
	}

	// must return a copy
	names[0] = "MODIFIED"
	fresh := LevelNames()
	if fresh[0] != "DEBUG" {
		t.Error("LevelNames() returned the same slice (not a copy)")
	}
}

func TestLookupLevel(t *testing.T) {
	cases := []struct {
		input   string
		want    slog.Level
		wantErr bool
	}{
		{"", LevelInfo, false},
		{"DEBUG", LevelDebug, false},
		{"debug", LevelDebug, false},
		{"INFO", LevelInfo, false},
		{"NOTICE", LevelNotice, false},
		{"WARNING", LevelWarning, false},
		{"WARN", LevelWarning, false},
		{"WaRnInG", LevelWarning, false},
		{"ERROR", LevelError, false},
		{"ERR", LevelError, false},
		{"FATAL", LevelError, false},
		{"EMERGENCY", LevelEmergency, false},
		{"  INFO  ", LevelInfo, false},
		{"INVALID", 0, true},
	}
	for _, c := range cases {
		got, err := LookupLevel(c.input)
		if c.wantErr {
			if err == nil {
				t.Errorf("LookupLevel(%q): expected error, got nil", c.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("LookupLevel(%q): unexpected error: %v", c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("LookupLevel(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestLevelSlogValue(t *testing.T) {
	cases := []struct {
		level slog.Level
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelNotice, "NOTICE"},
		{LevelWarning, "WARNING"},
		{LevelError, "ERROR"},
		{LevelEmergency, "EMERGENCY"},
		{slog.Level(999), "UNKNOWN"},
	}
	for _, c := range cases {
		if got := LevelSlogValue(c.level).String(); got != c.want {
			t.Errorf("LevelSlogValue(%v) = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestLevelString(t *testing.T) {
	cases := []struct {
		level slog.Level
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelNotice, "NOTICE"},
		{LevelWarning, "WARNING"},
		{LevelError, "ERROR"},
		{LevelEmergency, "EMERGENCY"},
		{slog.Level(999), "UNKNOWN"},
	}
	for _, c := range cases {
		if got := LevelString(c.level); got != c.want {
			t.Errorf("LevelString(%v) = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestLevelConstants(t *testing.T) {
	if LevelDebug != slog.Level(-4) {
		t.Errorf("LevelDebug = %v, want -4", LevelDebug)
	}
	if LevelInfo != slog.Level(0) {
		t.Errorf("LevelInfo = %v, want 0", LevelInfo)
	}
	if LevelNotice != slog.Level(2) {
		t.Errorf("LevelNotice = %v, want 2", LevelNotice)
	}
	if LevelWarning != slog.Level(4) {
		t.Errorf("LevelWarning = %v, want 4", LevelWarning)
	}
	if LevelError != slog.Level(8) {
		t.Errorf("LevelError = %v, want 8", LevelError)
	}
	if LevelEmergency != slog.Level(12) {
		t.Errorf("LevelEmergency = %v, want 12", LevelEmergency)
	}
}

func TestLevelOrdering(t *testing.T) {
	levels := []slog.Level{LevelDebug, LevelInfo, LevelNotice, LevelWarning, LevelError, LevelEmergency}
	for i := 0; i < len(levels)-1; i++ {
		if levels[i] >= levels[i+1] {
			t.Errorf("level[%d]=%v >= level[%d]=%v, expected strict ascending", i, levels[i], i+1, levels[i+1])
		}
	}
}
