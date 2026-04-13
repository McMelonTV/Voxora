//go:build !android

package authbridge

import (
	"os"
	"path/filepath"
	"strings"
)

func bridgeCredentialsFile() string {
	var path string
	if cacheDir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cacheDir) != "" {
		path = filepath.Join(cacheDir, "voxora", "spotify_credentials.json")
		_ = os.Setenv("LIBSPOTDL_CREDENTIALS_FILE", path)
		return path
	}
	if configDir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(configDir) != "" {
		path = filepath.Join(configDir, "voxora", "spotify_credentials.json")
		_ = os.Setenv("LIBSPOTDL_CREDENTIALS_FILE", path)
		return path
	}
	if homeDir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		path = filepath.Join(homeDir, ".voxora", "spotify_credentials.json")
		_ = os.Setenv("LIBSPOTDL_CREDENTIALS_FILE", path)
		return path
	}
	path = filepath.Join(os.TempDir(), "voxora", "spotify_credentials.json")
	_ = os.Setenv("LIBSPOTDL_CREDENTIALS_FILE", path)
	return path
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
