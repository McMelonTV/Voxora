//go:build android

package authbridge

import (
	"archive/zip"
	"bufio"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const androidAppID = "ing.boykiss.voxora"

func bridgeCredentialsFile() string {
	// Always target app-internal storage on Android to avoid scoped-storage permissions.
	path := filepath.Join("/data/data", androidAppID, "files", "voxora", "spotify_credentials.json")
	_ = os.Setenv("LIBSPOTDL_CREDENTIALS_FILE", path)
	return path
}

func existingFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func isNoExecAppFilesPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, prefix := range []string{
		filepath.Join("/data/data", androidAppID, "files") + "/",
		filepath.Join("/data/user/0", androidAppID, "files") + "/",
		filepath.Join("/data/user_de/0", androidAppID, "files") + "/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func hasPathEntry(pathValue, entry string) bool {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return false
	}
	for _, it := range strings.Split(pathValue, ":") {
		if strings.TrimSpace(it) == entry {
			return true
		}
	}
	return false
}

func androidABI() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64-v8a"
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	case "arm":
		return "armeabi-v7a"
	default:
		return ""
	}
}

func apkPathsFromProcessMaps() []string {
	file, err := os.Open("/proc/self/maps")
	if err != nil {
		return nil
	}
	defer file.Close()

	seen := map[string]struct{}{}
	out := make([]string, 0, 4)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		apkIdx := strings.Index(line, ".apk")
		if apkIdx == -1 {
			continue
		}
		prefix := line[:apkIdx+4]
		space := strings.LastIndex(prefix, " ")
		if space != -1 {
			prefix = strings.TrimSpace(prefix[space:])
		}
		if prefix == "" {
			continue
		}
		if _, ok := seen[prefix]; ok {
			continue
		}
		seen[prefix] = struct{}{}
		out = append(out, prefix)
	}

	return out
}

func extractFFmpegFromAPK() string {
	abi := androidABI()
	if abi == "" {
		return ""
	}

	cacheDir := filepath.Join(filepath.Dir(bridgeCredentialsFile()), "bin")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return ""
	}

	outPath := filepath.Join(cacheDir, "ffmpeg")
	if path := existingFile(outPath); path != "" {
		return path
	}

	apkCandidates := make([]string, 0, 4)
	if exe, err := os.Executable(); err == nil {
		exe = strings.TrimSpace(exe)
		if strings.HasSuffix(exe, ".apk") {
			apkCandidates = append(apkCandidates, exe)
		}
	}
	if matches, err := filepath.Glob("/data/app/*/base.apk"); err == nil {
		apkCandidates = append(apkCandidates, matches...)
	}
	if matches, err := filepath.Glob("/data/app/*/*/base.apk"); err == nil {
		apkCandidates = append(apkCandidates, matches...)
	}
	apkCandidates = append(apkCandidates, apkPathsFromProcessMaps()...)

	seen := map[string]struct{}{}
	entryNames := []string{
		"assets/ffmpeg/" + abi + "/ffmpeg",
		"lib/" + abi + "/libffmpeg_cli.so",
	}
	for _, apkPath := range apkCandidates {
		apkPath = strings.TrimSpace(apkPath)
		if apkPath == "" {
			continue
		}
		if _, ok := seen[apkPath]; ok {
			continue
		}
		seen[apkPath] = struct{}{}

		f, err := os.Open(apkPath)
		if err != nil {
			continue
		}
		stat, err := f.Stat()
		if err != nil {
			_ = f.Close()
			continue
		}

		zr, err := zip.NewReader(f, stat.Size())
		if err != nil {
			_ = f.Close()
			continue
		}

		for _, entryName := range entryNames {
			for _, zf := range zr.File {
				if zf.Name != entryName {
					continue
				}

				rc, err := zf.Open()
				if err != nil {
					break
				}

				tmpPath := outPath + ".part"
				outFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
				if err != nil {
					_ = rc.Close()
					break
				}

				_, copyErr := io.Copy(outFile, rc)
				_ = outFile.Close()
				_ = rc.Close()
				if copyErr != nil {
					_ = os.Remove(tmpPath)
					break
				}

				if err := os.Rename(tmpPath, outPath); err != nil {
					_ = os.Remove(tmpPath)
					break
				}
				_ = os.Chmod(outPath, 0o755)
				_ = f.Close()
				return existingFile(outPath)
			}
		}

		_ = f.Close()
	}

	return ""
}

func bridgePrepareFFmpegEnv() string {
	ffmpegPath := bridgeFFmpegPath()
	curPath := strings.TrimSpace(os.Getenv("PATH"))
	if curPath == "" {
		_ = os.Setenv("PATH", "/system/bin:/system/xbin")
	}
	if isNoExecAppFilesPath(ffmpegPath) {
		ffmpegPath = ""
	}

	if ffmpegPath != "" {
		_ = os.Setenv("VOXORA_FFMPEG_PATH", ffmpegPath)
		dir := filepath.Dir(ffmpegPath)
		curPath := os.Getenv("PATH")
		if !hasPathEntry(curPath, dir) {
			if strings.TrimSpace(curPath) == "" {
				_ = os.Setenv("PATH", dir)
			} else {
				_ = os.Setenv("PATH", dir+":"+curPath)
			}
		}
	}

	return ffmpegPath
}

func bridgeFFmpegPath() string {
	if envPath := strings.TrimSpace(os.Getenv("VOXORA_FFMPEG_PATH")); envPath != "" {
		if path := existingFile(envPath); path != "" {
			if !isNoExecAppFilesPath(path) {
				return path
			}
		}
	}

	for _, pattern := range []string{
		"/data/app/*/lib/*/libffmpeg_cli.so",
		"/data/app/*/*/lib/*/libffmpeg_cli.so",
		"/data/app/*" + androidAppID + "*/lib/*/libffmpeg_cli.so",
		"/data/app/*" + androidAppID + "*/*/lib/*/libffmpeg_cli.so",
	} {
		if matches, err := filepath.Glob(pattern); err == nil {
			for _, candidate := range matches {
				if path := existingFile(candidate); path != "" {
					return path
				}
			}
		}
	}

	// Ensure the packaged ffmpeg shared object is loaded so it appears in /proc/self/maps
	// and can be executed from its native library directory (which is executable on Android).
	_ = ensureFFmpegNativeLoaded()

	for _, varName := range []string{"LD_LIBRARY_PATH", "JAVA_LIBRARY_PATH"} {
		for _, dir := range strings.Split(os.Getenv(varName), ":") {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if path := existingFile(filepath.Join(dir, "libffmpeg_cli.so")); path != "" {
				return path
			}
		}
	}

	file, err := os.Open("/proc/self/maps")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	fallbackDir := ""
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "/libffmpeg_cli.so"); idx != -1 {
			start := strings.LastIndex(line[:idx], " ")
			if start == -1 {
				start = 0
			}
			path := strings.TrimSpace(line[start:])
			path = strings.TrimSuffix(path, " (deleted)")
			if resolved := existingFile(path); resolved != "" {
				return resolved
			}
		}

		if fallbackDir == "" {
			for _, marker := range []string{"/libMiqtGolangApp_", "/libQt6Core_", "/libplugins_platforms_qtforandroid_", "/libvoxora_"} {
				if idx := strings.Index(line, marker); idx != -1 {
					start := strings.LastIndex(line[:idx], " ")
					if start == -1 {
						start = 0
					}
					path := strings.TrimSpace(line[start:])
					path = strings.TrimSuffix(path, " (deleted)")
					if path != "" {
						fallbackDir = filepath.Dir(path)
					}
					break
				}
			}
		}
	}

	if fallbackDir != "" {
		candidate := filepath.Join(fallbackDir, "libffmpeg_cli.so")
		if path := existingFile(candidate); path != "" {
			return path
		}
	}

	return ""
}
