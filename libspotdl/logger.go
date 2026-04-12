package libspotdl

import (
	"fmt"
	stdlog "log"
	"sort"
	"strings"

	librespot "github.com/devgianlu/go-librespot"
)

type defaultLogger struct {
	fields map[string]any
	err    error
}

func newDefaultLogger() librespot.Logger {
	return &defaultLogger{}
}

func (l *defaultLogger) clone() *defaultLogger {
	clone := &defaultLogger{err: l.err}
	if len(l.fields) > 0 {
		clone.fields = make(map[string]any, len(l.fields))
		for k, v := range l.fields {
			clone.fields[k] = v
		}
	}
	return clone
}

func (l *defaultLogger) prefix(level string) string {
	parts := []string{level}
	if l.err != nil {
		parts = append(parts, fmt.Sprintf("error=%v", l.err))
	}
	if len(l.fields) > 0 {
		keys := make([]string, 0, len(l.fields))
		for key := range l.fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", key, l.fields[key]))
		}
	}
	return "[libspotdl " + strings.Join(parts, " ") + "] "
}

func (l *defaultLogger) logf(level, format string, args ...any) {
	stdlog.Printf("%s%s", l.prefix(level), fmt.Sprintf(format, args...))
}

func (l *defaultLogger) log(level string, args ...any) {
	stdlog.Print(l.prefix(level) + fmt.Sprint(args...))
}

func (l *defaultLogger) Tracef(format string, args ...interface{}) { l.logf("TRACE", format, args...) }
func (l *defaultLogger) Debugf(format string, args ...interface{}) { l.logf("DEBUG", format, args...) }
func (l *defaultLogger) Infof(format string, args ...interface{})  { l.logf("INFO", format, args...) }
func (l *defaultLogger) Warnf(format string, args ...interface{})  { l.logf("WARN", format, args...) }
func (l *defaultLogger) Errorf(format string, args ...interface{}) { l.logf("ERROR", format, args...) }

func (l *defaultLogger) Trace(args ...interface{}) { l.log("TRACE", args...) }
func (l *defaultLogger) Debug(args ...interface{}) { l.log("DEBUG", args...) }
func (l *defaultLogger) Info(args ...interface{})  { l.log("INFO", args...) }
func (l *defaultLogger) Warn(args ...interface{})  { l.log("WARN", args...) }
func (l *defaultLogger) Error(args ...interface{}) { l.log("ERROR", args...) }

func (l *defaultLogger) WithField(key string, value interface{}) librespot.Logger {
	clone := l.clone()
	if clone.fields == nil {
		clone.fields = map[string]any{}
	}
	clone.fields[key] = value
	return clone
}

func (l *defaultLogger) WithError(err error) librespot.Logger {
	clone := l.clone()
	clone.err = err
	return clone
}
