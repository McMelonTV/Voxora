package libspotdl

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"testing"

	librespotsession "github.com/devgianlu/go-librespot/session"
)

func TestWithSessionTroubleshootingHostsHint(t *testing.T) {
	err := withSessionTroubleshooting(errors.New(`failed getting accesspoint from resolver: failed fetching apresolve URL: Get "https://apresolve.spotify.com/?type=accesspoint": dial tcp 0.0.0.0:443: connect: connection refused`))
	if err == nil {
		t.Fatal("expected an error")
	}
	message := err.Error()
	if !strings.Contains(message, "Check /etc/hosts") {
		t.Fatalf("expected /etc/hosts hint, got: %s", message)
	}
	if !strings.Contains(message, "apresolve.spotify.com") {
		t.Fatalf("expected resolver hostname in hint, got: %s", message)
	}
}

func TestWithSessionTroubleshootingLeavesOtherErrorsAlone(t *testing.T) {
	original := errors.New("some other auth failure")
	if got := withSessionTroubleshooting(original); got != original {
		t.Fatalf("expected original error to be returned unchanged")
	}
}

func TestResolveSessionCredentialsUsesCachedStoredCredentials(t *testing.T) {
	cache := &credentialCache{
		Username:          "spotify-user",
		StoredCredentials: base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4}),
	}

	creds, err := resolveSessionCredentials(context.Background(), Config{}, newDefaultLogger(), nil, "0123456789abcdef0123456789abcdef01234567", cache)
	if err != nil {
		t.Fatalf("resolveSessionCredentials returned error: %v", err)
	}

	stored, ok := creds.(librespotsession.StoredCredentials)
	if !ok {
		t.Fatalf("resolveSessionCredentials returned %T, want StoredCredentials", creds)
	}
	if stored.Username != cache.Username {
		t.Fatalf("stored username = %q, want %q", stored.Username, cache.Username)
	}
	if got := base64.StdEncoding.EncodeToString(stored.Data); got != cache.StoredCredentials {
		t.Fatalf("stored credential bytes = %q, want %q", got, cache.StoredCredentials)
	}
}

func TestResolveSessionCredentialsAllowsTokenAuthWithoutUsername(t *testing.T) {
	creds, err := resolveSessionCredentials(context.Background(), Config{
		Auth: AuthConfig{AccessToken: "token-without-username"},
	}, newDefaultLogger(), nil, "0123456789abcdef0123456789abcdef01234567", nil)
	if err != nil {
		t.Fatalf("resolveSessionCredentials returned error: %v", err)
	}
	tokenCreds, ok := creds.(librespotsession.SpotifyTokenCredentials)
	if !ok {
		t.Fatalf("resolveSessionCredentials returned %T, want SpotifyTokenCredentials", creds)
	}
	if tokenCreds.Username != "" {
		t.Fatalf("spotify token username = %q, want empty", tokenCreds.Username)
	}
	if tokenCreds.Token != "token-without-username" {
		t.Fatalf("spotify token = %q, want %q", tokenCreds.Token, "token-without-username")
	}
}

func TestRandomDeviceIDReturnsHexValue(t *testing.T) {
	deviceID, err := randomDeviceID()
	if err != nil {
		t.Fatalf("randomDeviceID returned error: %v", err)
	}

	pattern := regexp.MustCompile(`^[0-9a-f]{40}$`)
	if !pattern.MatchString(deviceID) {
		t.Fatalf("randomDeviceID = %q, want 40-character lowercase hex format", deviceID)
	}
}

func TestIsLegacyHexDeviceID(t *testing.T) {
	if !isLegacyHexDeviceID("0123456789abcdef0123456789abcdef01234567") {
		t.Fatal("expected 40-character hex device ID to be treated as legacy")
	}
	if isLegacyHexDeviceID("550e8400-e29b-41d4-a716-446655440000") {
		t.Fatal("did not expect UUID-like device ID to be treated as legacy")
	}
}
