//go:build !android

package authbridge

import (
	"path/filepath"
	"testing"
)

func TestResolveBridgeCredentialsFilePrefersConfigDir(t *testing.T) {
	got := resolveBridgeCredentialsFile("/config-dir", "/home-dir", "/tmp-dir")
	want := filepath.Join("/config-dir", "voxora", "spotify_credentials.json")
	if got != want {
		t.Fatalf("resolveBridgeCredentialsFile = %q, want %q", got, want)
	}
}

func TestResolveBridgeCredentialsFileFallsBackToHomeDir(t *testing.T) {
	got := resolveBridgeCredentialsFile("", "/home-dir", "/tmp-dir")
	want := filepath.Join("/home-dir", ".voxora", "spotify_credentials.json")
	if got != want {
		t.Fatalf("resolveBridgeCredentialsFile = %q, want %q", got, want)
	}
}

func TestLegacyBridgeCredentialsFileUsesCacheDir(t *testing.T) {
	got := legacyBridgeCredentialsFile("/cache-dir")
	want := filepath.Join("/cache-dir", "voxora", "spotify_credentials.json")
	if got != want {
		t.Fatalf("legacyBridgeCredentialsFile = %q, want %q", got, want)
	}
}
