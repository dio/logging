package logging

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

// GCP-compatible severity levels as slog.Level constants.
// These map 1:1 to Cloud Logging severity and fit into slog's int space.
// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity
const (
	LevelDebug     = slog.Level(-4) // DEBUG
	LevelInfo      = slog.Level(0)  // INFO
	LevelNotice    = slog.Level(2)  // NOTICE    (normal but significant)
	LevelWarning   = slog.Level(4)  // WARNING   (might cause problems)
	LevelError     = slog.Level(8)  // ERROR     (likely to cause problems)
	LevelEmergency = slog.Level(12) // EMERGENCY (system is unusable)
)

const (
	levelUnknownName   = "UNKNOWN"
	levelDebugName     = "DEBUG"
	levelInfoName      = "INFO"
	levelNoticeName    = "NOTICE"
	levelWarningName   = "WARNING"
	levelErrorName     = "ERROR"
	levelEmergencyName = "EMERGENCY"
)

// pre-computed slog.Value instances — avoid allocation on every log call.
var (
	levelUnknownSlogValue   = slog.StringValue(levelUnknownName)
	levelDebugSlogValue     = slog.StringValue(levelDebugName)
	levelInfoSlogValue      = slog.StringValue(levelInfoName)
	levelNoticeSlogValue    = slog.StringValue(levelNoticeName)
	levelWarningSlogValue   = slog.StringValue(levelWarningName)
	levelErrorSlogValue     = slog.StringValue(levelErrorName)
	levelEmergencySlogValue = slog.StringValue(levelEmergencyName)
)

var levelNames = []string{
	levelDebugName,
	levelInfoName,
	levelNoticeName,
	levelWarningName,
	levelErrorName,
	levelEmergencyName,
}

// LevelNames returns a copy of all supported log level names in ascending order.
func LevelNames() []string {
	return slices.Clone(levelNames)
}

// LookupLevel resolves a level name to a slog.Level.
// Matching is case-insensitive and leading/trailing whitespace is trimmed.
// The empty string resolves to LevelInfo.
// Aliases: WARN -> LevelWarning, ERR/FATAL -> LevelError.
func LookupLevel(name string) (slog.Level, error) {
	switch v := strings.ToUpper(strings.TrimSpace(name)); v {
	case "":
		return LevelInfo, nil
	case levelDebugName:
		return LevelDebug, nil
	case levelInfoName:
		return LevelInfo, nil
	case levelNoticeName:
		return LevelNotice, nil
	case levelWarningName, "WARN":
		return LevelWarning, nil
	case levelErrorName, "ERR", "FATAL":
		return LevelError, nil
	case levelEmergencyName:
		return LevelEmergency, nil
	default:
		return 0, fmt.Errorf("no such level %q, valid levels are %s", name, strings.Join(levelNames, ", "))
	}
}

// LevelSlogValue returns the pre-allocated slog.Value string for l.
// Unknown levels return "UNKNOWN".
func LevelSlogValue(l slog.Level) slog.Value {
	switch l {
	case LevelDebug:
		return levelDebugSlogValue
	case LevelInfo:
		return levelInfoSlogValue
	case LevelNotice:
		return levelNoticeSlogValue
	case LevelWarning:
		return levelWarningSlogValue
	case LevelError:
		return levelErrorSlogValue
	case LevelEmergency:
		return levelEmergencySlogValue
	default:
		return levelUnknownSlogValue
	}
}

// LevelString returns the canonical string name for l.
// Unlike l.String(), this always returns one of the six GCP severity names
// (or "UNKNOWN" for values not in the set).
func LevelString(l slog.Level) string {
	switch l {
	case LevelDebug:
		return levelDebugName
	case LevelInfo:
		return levelInfoName
	case LevelNotice:
		return levelNoticeName
	case LevelWarning:
		return levelWarningName
	case LevelError:
		return levelErrorName
	case LevelEmergency:
		return levelEmergencyName
	default:
		return levelUnknownName
	}
}
