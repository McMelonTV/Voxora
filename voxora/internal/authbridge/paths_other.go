//go:build !android

package authbridge

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

func bridgeCredentialsFile() string {
	configDir, _ := os.UserConfigDir()
	cacheDir, _ := os.UserCacheDir()
	homeDir, _ := os.UserHomeDir()
	path := resolveBridgeCredentialsFile(configDir, homeDir, os.TempDir())
	migrateLegacyBridgeCredentialsFile(legacyBridgeCredentialsFile(cacheDir), path)
	_ = os.Setenv("LIBSPOTDL_CREDENTIALS_FILE", path)
	return path
}

func resolveBridgeCredentialsFile(configDir, homeDir, tempDir string) string {
	if configDir = strings.TrimSpace(configDir); configDir != "" {
		return filepath.Join(configDir, "voxora", "spotify_credentials.json")
	}
	if homeDir = strings.TrimSpace(homeDir); homeDir != "" {
		return filepath.Join(homeDir, ".voxora", "spotify_credentials.json")
	}
	return filepath.Join(tempDir, "voxora", "spotify_credentials.json")
}

func legacyBridgeCredentialsFile(cacheDir string) string {
	if cacheDir = strings.TrimSpace(cacheDir); cacheDir == "" {
		return ""
	}
	return filepath.Join(cacheDir, "voxora", "spotify_credentials.json")
}

func migrateLegacyBridgeCredentialsFile(legacyPath, targetPath string) {
	legacyPath = strings.TrimSpace(legacyPath)
	targetPath = strings.TrimSpace(targetPath)
	if legacyPath == "" || targetPath == "" || legacyPath == targetPath {
		return
	}
	if _, err := os.Stat(targetPath); err == nil {
		return
	}
	if _, err := os.Stat(legacyPath); err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return
	}
	if err := os.Rename(legacyPath, targetPath); err == nil {
		return
	}
	copyFile(legacyPath, targetPath)
}

func copyFile(srcPath, dstPath string) {
	src, err := os.Open(srcPath)
	if err != nil {
		return
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	defer dst.Close()

	_, _ = io.Copy(dst, src)
}

func bridgeFFmpegPath() string {
	if envPath := strings.TrimSpace(os.Getenv("VOXORA_FFMPEG_PATH")); envPath != "" {
		if info, err := os.Stat(envPath); err == nil && !info.IsDir() {
			return envPath
		}
	}
	return ""
}

func bridgePrepareFFmpegEnv() string {
	path := bridgeFFmpegPath()
	if path != "" {
		_ = os.Setenv("VOXORA_FFMPEG_PATH", path)
	}
	return path
}
