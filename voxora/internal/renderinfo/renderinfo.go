package renderinfo

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const probeTimeout = 500 * time.Millisecond

type Snapshot struct {
	Platform string
	API      string
	Renderer string
}

func Collect(platform string) Snapshot {
	snapshot := Snapshot{
		Platform: normalizePlatform(platform),
		API:      "Unknown",
		Renderer: "Unknown",
	}

	for _, probe := range probeOrder(snapshot.Platform) {
		if parsed, err := probe(snapshot.Platform); err == nil {
			return parsed
		}
	}

	return snapshot
}

func ParseEGLInfo(out string, platform string) (Snapshot, error) {
	target := eglSectionName(platform)
	current := ""
	snapshot := Snapshot{Platform: normalizePlatform(platform)}

	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if strings.HasSuffix(line, "platform:") {
			current = strings.ToLower(line)
			continue
		}

		if current != target {
			continue
		}

		switch {
		case strings.HasPrefix(line, "OpenGL core profile renderer:"):
			snapshot.API = "OpenGL (EGL)"
			snapshot.Renderer = strings.TrimSpace(strings.TrimPrefix(line, "OpenGL core profile renderer:"))
		case snapshot.Renderer == "" && strings.HasPrefix(line, "OpenGL renderer string:"):
			snapshot.API = "OpenGL (EGL)"
			snapshot.Renderer = strings.TrimSpace(strings.TrimPrefix(line, "OpenGL renderer string:"))
		case snapshot.Renderer == "" && strings.HasPrefix(line, "OpenGL ES profile renderer:"):
			snapshot.API = "OpenGL ES (EGL)"
			snapshot.Renderer = strings.TrimSpace(strings.TrimPrefix(line, "OpenGL ES profile renderer:"))
		}
	}

	if snapshot.Renderer == "" {
		return Snapshot{}, errors.New("renderer not found in eglinfo output")
	}

	return snapshot, nil
}

func ParseGLXInfo(out string, platform string) (Snapshot, error) {
	snapshot := Snapshot{Platform: normalizePlatform(platform), API: "OpenGL (GLX)"}

	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "OpenGL renderer string:") {
			snapshot.Renderer = strings.TrimSpace(strings.TrimPrefix(line, "OpenGL renderer string:"))
			break
		}
	}

	if snapshot.Renderer == "" {
		return Snapshot{}, errors.New("renderer not found in glxinfo output")
	}

	return snapshot, nil
}

func normalizePlatform(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		return "unknown"
	}
	if strings.Contains(platform, "xcb") || strings.Contains(platform, "x11") {
		return "xcb"
	}
	if strings.Contains(platform, "wayland") {
		return "wayland"
	}
	return platform
}

func eglSectionName(platform string) string {
	switch normalizePlatform(platform) {
	case "xcb":
		return "x11 platform:"
	case "wayland":
		return "wayland platform:"
	case "offscreen":
		return "surfaceless platform:"
	default:
		return normalizePlatform(platform) + " platform:"
	}
}

func probeOrder(platform string) []func(string) (Snapshot, error) {
	switch normalizePlatform(platform) {
	case "xcb":
		return []func(string) (Snapshot, error){probeGLXInfo, probeEGLInfo}
	default:
		return []func(string) (Snapshot, error){probeEGLInfo, probeGLXInfo}
	}
}

func probeEGLInfo(platform string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "eglinfo", "-B").CombinedOutput()
	if err != nil {
		return Snapshot{}, err
	}

	return ParseEGLInfo(string(out), platform)
}

func probeGLXInfo(platform string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "glxinfo", "-B").CombinedOutput()
	if err != nil {
		return Snapshot{}, err
	}

	return ParseGLXInfo(string(out), platform)
}
