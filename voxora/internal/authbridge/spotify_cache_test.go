//go:build !android

package authbridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStreamCacheProtectedPathsForPlayFileIncludeCachedTwin(t *testing.T) {
	playPath := filepath.Join(t.TempDir(), "track.stream.play.ogg")
	protected := streamCacheProtectedPaths(playPath)

	if _, ok := protected[playPath]; !ok {
		t.Fatalf("expected protected paths to include active play path %q", playPath)
	}

	cachePath := filepath.Join(filepath.Dir(playPath), "track.stream.ogg")
	if _, ok := protected[cachePath]; !ok {
		t.Fatalf("expected protected paths to include cached twin %q", cachePath)
	}
}

func TestCleanupStreamCacheArtifactsRemovesStaleFilesButKeepsProtectedPair(t *testing.T) {
	cacheDir := t.TempDir()

	activePlay := filepath.Join(cacheDir, "active.stream.play.ogg")
	activeCache := filepath.Join(cacheDir, "active.stream.ogg")
	activeDone := streamCacheDoneMarkerPathForStream(activeCache)
	stalePlay := filepath.Join(cacheDir, "stale.stream.play.ogg")
	staleCache := filepath.Join(cacheDir, "stale.stream.ogg")
	staleDone := streamCacheDoneMarkerPathForStream(staleCache)
	orphanDone := filepath.Join(cacheDir, "orphan.stream.ogg.done")
	prefetchPart := filepath.Join(cacheDir, "next.prefetch.part")

	files := []string{activePlay, activeCache, activeDone, stalePlay, staleCache, staleDone, orphanDone, prefetchPart}
	for _, path := range files {
		if err := os.WriteFile(path, []byte("cache-data"), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}

	removed := cleanupStreamCacheArtifacts(cacheDir, streamCacheProtectedPaths(activePlay))
	if removed < 4 {
		t.Fatalf("cleanup removed %d files, want at least 4 stale artifacts removed", removed)
	}

	for _, keepPath := range []string{activePlay, activeCache, activeDone} {
		if _, err := os.Stat(keepPath); err != nil {
			t.Fatalf("expected %q to be preserved: %v", keepPath, err)
		}
	}

	for _, removedPath := range []string{stalePlay, staleCache, staleDone, orphanDone, prefetchPart} {
		if _, err := os.Stat(removedPath); !os.IsNotExist(err) {
			t.Fatalf("expected %q to be removed, stat err=%v", removedPath, err)
		}
	}
}

func TestTouchStreamCacheEntryTouchesDoneMarker(t *testing.T) {
	cacheDir := t.TempDir()
	streamPath := filepath.Join(cacheDir, "track.stream.ogg")
	donePath := streamCacheDoneMarkerPathForStream(streamPath)

	if err := os.WriteFile(streamPath, []byte("cached-track"), 0o644); err != nil {
		t.Fatalf("write stream file: %v", err)
	}
	if err := os.WriteFile(donePath, []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write done marker: %v", err)
	}

	before, err := os.Stat(donePath)
	if err != nil {
		t.Fatalf("stat done marker before touch: %v", err)
	}

	touchStreamCacheEntry(streamPath)

	after, err := os.Stat(donePath)
	if err != nil {
		t.Fatalf("stat done marker after touch: %v", err)
	}
	if after.ModTime().Before(before.ModTime()) {
		t.Fatalf("done marker mod time moved backwards: before=%v after=%v", before.ModTime(), after.ModTime())
	}
}
