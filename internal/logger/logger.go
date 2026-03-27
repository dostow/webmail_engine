package logger

import (
	"fmt"
	"log"
	"time"
)

// Logger defines the interface for structured logging.
// This allows dependency injection of custom loggers (e.g., zap, logrus).
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
}

// StandardLogger is a simple logger that wraps the standard log package.
type StandardLogger struct{}

func (l *StandardLogger) Info(msg string, keysAndValues ...interface{}) {
	l.log("INFO", msg, keysAndValues...)
}

func (l *StandardLogger) Error(msg string, keysAndValues ...interface{}) {
	l.log("ERROR", msg, keysAndValues...)
}

func (l *StandardLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.log("DEBUG", msg, keysAndValues...)
}

func (l *StandardLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.log("WARN", msg, keysAndValues...)
}

func (l *StandardLogger) log(level, msg string, keysAndValues ...interface{}) {
	prefix := fmt.Sprintf("[%s] %s: %s", level, time.Now().Format(time.RFC3339), msg)

	if len(keysAndValues) == 0 {
		log.Print(prefix)
		return
	}

	// Format key-value pairs properly
	kvStr := formatKeyValuePairs(keysAndValues)
	log.Printf("%s %s", prefix, kvStr)
}

// formatKeyValuePairs formats key-value pairs in a readable "key=value" format.
// If the number of keysAndValues is odd, the last value is printed as-is.
func formatKeyValuePairs(keysAndValues []interface{}) string {
	if len(keysAndValues) == 0 {
		return ""
	}

	result := make([]string, 0, len(keysAndValues)/2+1)

	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			// We have both key and value
			result = append(result, fmt.Sprintf("%v=%v", keysAndValues[i], keysAndValues[i+1]))
		} else {
			// Odd number of arguments, print the last one as-is
			result = append(result, fmt.Sprintf("%v", keysAndValues[i]))
		}
	}

	return fmt.Sprintf("[%s]", joinStrings(result, " "))
}

// joinStrings joins a slice of strings with the given separator.
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}

// NewStandardLogger creates a new StandardLogger instance.
func NewStandardLogger() *StandardLogger {
	return &StandardLogger{}
}
