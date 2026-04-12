package libspotdl

import "testing"

func TestResolveCredentialsFileReturnsDefault(t *testing.T) {
	path, err := ResolveCredentialsFile("")
	if err != nil {
		t.Fatalf("ResolveCredentialsFile returned error: %v", err)
	}
	if path == "" {
		t.Fatal("expected a non-empty credentials path")
	}
}
