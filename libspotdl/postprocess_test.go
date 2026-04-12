package libspotdl

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type ffprobeResult struct {
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		Disposition map[string]int    `json:"disposition"`
		Tags        map[string]string `json:"tags"`
	} `json:"streams"`
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

func requireCommand(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not available: %v", name, err)
	}
	return path
}

func runCommand(t *testing.T, timeout time.Duration, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, strings.TrimSpace(string(output)))
	}
}

func probeFile(t *testing.T, ffprobePath, path string) ffprobeResult {
	t.Helper()
	cmd := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "format_tags=title,artist,album,track,disc,date:stream=codec_type,disposition:stream_tags=title,comment",
		"-of", "json",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe failed: %v", err)
	}

	var result ffprobeResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode ffprobe output: %v", err)
	}
	return result
}

func TestBuildFFmpegArgsForMP3Artwork(t *testing.T) {
	args := buildFFmpegArgs(ffmpegJob{
		SourcePath:   "/tmp/source.ogg",
		OutputPath:   "/tmp/final.mp3",
		OutputFormat: OutputFormatMP3,
		ArtworkPath:  "/tmp/cover.jpg",
		Tagging: TaggingConfig{
			Enabled:        true,
			IncludeArtwork: true,
		},
		Metadata: MediaMetadata{
			Title:       "Song",
			Artists:     []string{"Artist"},
			Album:       "Album",
			ReleaseDate: "2026-04-11",
		},
	})

	joined := strings.Join(args, " ")
	for _, want := range []string{"-c:a libmp3lame", "-b:a 320k", "-f mp3", "-id3v2_version 3", "title=Song", "artist=Artist", "/tmp/final.mp3"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ffmpeg args %q do not contain %q", joined, want)
		}
	}
}

func TestOutputMuxerSourceByOutputExtension(t *testing.T) {
	if got := outputMuxer(OutputFormatSource, "/tmp/out.ogg.ffmpeg.part", "ogg"); got != "ogg" {
		t.Fatalf("outputMuxer(source, ogg) = %q, want %q", got, "ogg")
	}
	if got := outputMuxer(OutputFormatSource, "/tmp/out.aac.ffmpeg.part", "aac"); got != "adts" {
		t.Fatalf("outputMuxer(source, aac) = %q, want %q", got, "adts")
	}
}

func TestLibrespotIDFromURI(t *testing.T) {
	id, err := librespotIDFromURI("spotify:track:11dFghVXANMlKmJXsNCbNl")
	if err != nil {
		t.Fatalf("librespotIDFromURI returned error: %v", err)
	}
	if id != "11dFghVXANMlKmJXsNCbNl" {
		t.Fatalf("id = %q, want %q", id, "11dFghVXANMlKmJXsNCbNl")
	}
}

func TestRunFFmpegPostProcessMP3WithArtwork(t *testing.T) {
	ffmpegPath := requireCommand(t, "ffmpeg")
	ffprobePath := requireCommand(t, "ffprobe")
	tmpDir := t.TempDir()

	sourcePath := filepath.Join(tmpDir, "source.wav")
	coverPath := filepath.Join(tmpDir, "cover.jpg")
	outputPath := filepath.Join(tmpDir, "output.mp3")

	runCommand(t, 30*time.Second, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=1000:duration=1",
		"-c:a", "pcm_s16le",
		sourcePath,
	)
	runCommand(t, 30*time.Second, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=red:s=32x32:d=0.1",
		"-frames:v", "1",
		coverPath,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runFFmpegPostProcess(ctx, ffmpegPath, ffmpegJob{
		SourcePath:   sourcePath,
		OutputPath:   outputPath,
		OutputFormat: OutputFormatMP3,
		ArtworkPath:  coverPath,
		Tagging: TaggingConfig{
			Enabled:        true,
			IncludeArtwork: true,
		},
		Metadata: MediaMetadata{
			URI:         "spotify:track:test",
			Title:       "Test Song",
			Artists:     []string{"Artist One", "Artist Two"},
			Album:       "Test Album",
			ReleaseDate: "2026-04-11",
			TrackNumber: 7,
			DiscNumber:  2,
		},
	}); err != nil {
		t.Fatalf("runFFmpegPostProcess returned error: %v", err)
	}

	probe := probeFile(t, ffprobePath, outputPath)
	if probe.Format.Tags["title"] != "Test Song" {
		t.Fatalf("title = %q, want %q", probe.Format.Tags["title"], "Test Song")
	}
	if probe.Format.Tags["artist"] != "Artist One, Artist Two" {
		t.Fatalf("artist = %q, want %q", probe.Format.Tags["artist"], "Artist One, Artist Two")
	}
	if probe.Format.Tags["album"] != "Test Album" {
		t.Fatalf("album = %q, want %q", probe.Format.Tags["album"], "Test Album")
	}

	hasArtwork := false
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			hasArtwork = true
			break
		}
		if stream.Disposition["attached_pic"] == 1 {
			hasArtwork = true
			break
		}
	}
	if !hasArtwork {
		t.Fatal("expected embedded artwork stream in mp3 output")
	}
}

func TestRunFFmpegPostProcessFLACWithArtwork(t *testing.T) {
	ffmpegPath := requireCommand(t, "ffmpeg")
	ffprobePath := requireCommand(t, "ffprobe")
	tmpDir := t.TempDir()

	sourcePath := filepath.Join(tmpDir, "source.wav")
	coverPath := filepath.Join(tmpDir, "cover.jpg")
	outputPath := filepath.Join(tmpDir, "output.flac")

	runCommand(t, 30*time.Second, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=700:duration=1",
		"-c:a", "pcm_s16le",
		sourcePath,
	)
	runCommand(t, 30*time.Second, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=blue:s=32x32:d=0.1",
		"-frames:v", "1",
		coverPath,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runFFmpegPostProcess(ctx, ffmpegPath, ffmpegJob{
		SourcePath:   sourcePath,
		OutputPath:   outputPath,
		OutputFormat: OutputFormatFLAC,
		ArtworkPath:  coverPath,
		Tagging: TaggingConfig{
			Enabled:        true,
			IncludeArtwork: true,
		},
		Metadata: MediaMetadata{
			URI:         "spotify:track:test",
			Title:       "Flac Song",
			Artists:     []string{"Artist FLAC"},
			Album:       "Flac Album",
			ReleaseDate: "2026-04-11",
		},
	}); err != nil {
		t.Fatalf("runFFmpegPostProcess returned error: %v", err)
	}

	probe := probeFile(t, ffprobePath, outputPath)
	if probe.Format.Tags["title"] != "Flac Song" {
		t.Fatalf("title = %q, want %q", probe.Format.Tags["title"], "Flac Song")
	}
	if probe.Format.Tags["artist"] != "Artist FLAC" {
		t.Fatalf("artist = %q, want %q", probe.Format.Tags["artist"], "Artist FLAC")
	}

	hasArtwork := false
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" || stream.Disposition["attached_pic"] == 1 {
			hasArtwork = true
			break
		}
	}
	if !hasArtwork {
		t.Fatal("expected embedded artwork stream in flac output")
	}
}
