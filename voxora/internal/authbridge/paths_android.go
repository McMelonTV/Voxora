//go:build android

package authbridge

import (
	"os"
	"path/filepath"
)

const androidAppID = "ing.boykiss.voxora"

func bridgeCredentialsFile() string {
	// Always target app-internal storage on Android to avoid scoped-storage permissions.
	path := filepath.Join("/data/data", androidAppID, "files", "voxora", "spotify_credentials.json")
	_ = os.Setenv("LIBSPOTDL_CREDENTIALS_FILE", path)
	return path
}
