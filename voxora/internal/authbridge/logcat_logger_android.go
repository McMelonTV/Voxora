//go:build android

package authbridge

/*
#include <android/log.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"sort"
	"strings"
	"unsafe"

	librespot "github.com/devgianlu/go-librespot"
)

const (
	androidLogVerbose = 2
	androidLogDebug   = 3
	androidLogInfo    = 4
	androidLogWarn    = 5
	androidLogError   = 6
)

type logcatLogger struct {
	tag    string
	fields map[string]any
	err    error
}

func newBridgeLogger() librespot.Logger {
	return &logcatLogger{tag: "VoxoraAuth"}
}

func (l *logcatLogger) clone() *logcatLogger {
	c := &logcatLogger{tag: l.tag, err: l.err}
	if len(l.fields) > 0 {
		c.fields = make(map[string]any, len(l.fields))
		for k, v := range l.fields {
			c.fields[k] = v
		}
	}
	return c
}

func (l *logcatLogger) prefix(level string) string {
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
	return "[" + strings.Join(parts, " ") + "] "
}

func (l *logcatLogger) write(priority C.int, message string) {
	tag := C.CString(l.tag)
	msg := C.CString(message)
	defer C.free(unsafe.Pointer(tag))
	defer C.free(unsafe.Pointer(msg))
	C.__android_log_write(priority, tag, msg)
}

func (l *logcatLogger) logf(level string, priority C.int, format string, args ...any) {
	l.write(priority, l.prefix(level)+fmt.Sprintf(format, args...))
}

func (l *logcatLogger) log(level string, priority C.int, args ...any) {
	l.write(priority, l.prefix(level)+fmt.Sprint(args...))
}

func (l *logcatLogger) Tracef(format string, args ...interface{}) {
	l.logf("TRACE", androidLogVerbose, format, args...)
}
func (l *logcatLogger) Debugf(format string, args ...interface{}) {
	l.logf("DEBUG", androidLogDebug, format, args...)
}
func (l *logcatLogger) Infof(format string, args ...interface{}) {
	l.logf("INFO", androidLogInfo, format, args...)
}
func (l *logcatLogger) Warnf(format string, args ...interface{}) {
	l.logf("WARN", androidLogWarn, format, args...)
}
func (l *logcatLogger) Errorf(format string, args ...interface{}) {
	l.logf("ERROR", androidLogError, format, args...)
}

func (l *logcatLogger) Trace(args ...interface{}) { l.log("TRACE", androidLogVerbose, args...) }
func (l *logcatLogger) Debug(args ...interface{}) { l.log("DEBUG", androidLogDebug, args...) }
func (l *logcatLogger) Info(args ...interface{})  { l.log("INFO", androidLogInfo, args...) }
func (l *logcatLogger) Warn(args ...interface{})  { l.log("WARN", androidLogWarn, args...) }
func (l *logcatLogger) Error(args ...interface{}) { l.log("ERROR", androidLogError, args...) }

func (l *logcatLogger) WithField(key string, value interface{}) librespot.Logger {
	clone := l.clone()
	if clone.fields == nil {
		clone.fields = map[string]any{}
	}
	clone.fields[key] = value
	return clone
}

func (l *logcatLogger) WithError(err error) librespot.Logger {
	clone := l.clone()
	clone.err = err
	return clone
}
