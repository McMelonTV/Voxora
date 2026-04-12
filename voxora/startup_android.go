//go:build android
// +build android

package main

/*
#include <android/log.h>
#include <stdlib.h>
*/
import "C"

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"unsafe"
)

const (
	androidLogVerbose = 2
	androidLogDebug   = 3
	androidLogInfo    = 4
	androidLogWarn    = 5
	androidLogError   = 6
)

var logcatMu sync.Mutex

func writeLogcat(priority C.int, tag, message string) {
	clean := strings.TrimSpace(message)
	if clean == "" {
		return
	}
	cTag := C.CString(tag)
	cMsg := C.CString(clean)
	defer C.free(unsafe.Pointer(cTag))
	defer C.free(unsafe.Pointer(cMsg))

	logcatMu.Lock()
	C.__android_log_write(priority, cTag, cMsg)
	logcatMu.Unlock()
}

type logcatWriter struct {
	priority C.int
	tag      string
}

func (w logcatWriter) Write(p []byte) (int, error) {
	text := string(p)
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		writeLogcat(w.priority, w.tag, line)
	}
	return len(p), nil
}

func mirrorPipeToLogcat(r *os.File, priority C.int, tag, prefix string) {
	go func() {
		defer r.Close()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			if prefix != "" {
				line = prefix + line
			}
			writeLogcat(priority, tag, line)
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			writeLogcat(androidLogError, tag, fmt.Sprintf("log stream scanner error: %v", err))
		}
	}()
}

func routeGoLogsToLogcat() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(logcatWriter{priority: androidLogInfo, tag: "VoxoraGo"})

	outR, outW, err := os.Pipe()
	if err == nil {
		os.Stdout = outW
		mirrorPipeToLogcat(outR, androidLogInfo, "VoxoraGo", "stdout: ")
	}

	errR, errW, err := os.Pipe()
	if err == nil {
		os.Stderr = errW
		mirrorPipeToLogcat(errR, androidLogError, "VoxoraGo", "stderr: ")
	}

	writeLogcat(androidLogInfo, "VoxoraGo", "go logcat routing initialized")
}

func forceAndroidSystemDNSResolver() {
	const value = "netdns=cgo"
	current := strings.TrimSpace(os.Getenv("GODEBUG"))
	if current == "" {
		_ = os.Setenv("GODEBUG", value)
		return
	}
	if strings.Contains(current, "netdns=") {
		return
	}
	_ = os.Setenv("GODEBUG", current+","+value)
}

//export AndroidMain
func AndroidMain() {
	forceAndroidSystemDNSResolver()
	routeGoLogsToLogcat()
	app_main()
}

func main() {
	// Must be empty.
}
