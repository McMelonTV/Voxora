package libspotdl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ffmpegJob struct {
	SourcePath   string
	OutputPath   string
	OutputFormat OutputFormat
	Metadata     MediaMetadata
	Tagging      TaggingConfig
	ArtworkPath  string
}

func normalizeOutputFormat(format OutputFormat) OutputFormat {
	switch format {
	case "", OutputFormatSource:
		return OutputFormatSource
	case OutputFormatMP3:
		return OutputFormatMP3
	case OutputFormatFLAC:
		return OutputFormatFLAC
	default:
		return OutputFormat("")
	}
}

func outputExtension(format OutputFormat, metadata MediaMetadata) string {
	switch normalizeOutputFormat(format) {
	case OutputFormatMP3:
		return "mp3"
	case OutputFormatFLAC:
		return "flac"
	default:
		return metadata.FileExtension
	}
}

func needsPostProcess(req Request) bool {
	return normalizeOutputFormat(req.OutputFormat) != OutputFormatSource || req.Tagging.Enabled || req.Tagging.IncludeArtwork
}

func canEmbedArtwork(format OutputFormat, outputPath string) bool {
	switch normalizeOutputFormat(format) {
	case OutputFormatMP3, OutputFormatFLAC:
		return true
	case OutputFormatSource:
		switch strings.ToLower(filepath.Ext(outputPath)) {
		case ".mp3", ".flac":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func metadataArgs(metadata MediaMetadata) []string {
	album := metadata.Album
	if album == "" {
		album = metadata.Show
	}

	args := []string{
		"-metadata", "title=" + metadata.Title,
		"-metadata", "artist=" + strings.Join(metadata.Artists, ", "),
		"-metadata", "album=" + album,
		"-metadata", "comment=Downloaded by libspotdl from " + metadata.URI,
	}

	if metadata.ReleaseDate != "" {
		args = append(args, "-metadata", "date="+metadata.ReleaseDate)
	}
	if metadata.TrackNumber > 0 {
		args = append(args, "-metadata", fmt.Sprintf("track=%d", metadata.TrackNumber))
	}
	if metadata.DiscNumber > 0 {
		args = append(args, "-metadata", fmt.Sprintf("disc=%d", metadata.DiscNumber))
	}

	return args
}

func buildFFmpegArgs(job ffmpegJob) []string {
	format := normalizeOutputFormat(job.OutputFormat)
	if format == "" {
		format = OutputFormatSource
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-i", job.SourcePath}
	embedArtwork := job.ArtworkPath != "" && canEmbedArtwork(format, job.OutputPath)
	if embedArtwork {
		args = append(args, "-i", job.ArtworkPath)
	}

	args = append(args, "-map", "0:a:0")
	if embedArtwork {
		args = append(args, "-map", "1:v:0")
	}

	switch format {
	case OutputFormatMP3:
		// Use CBR 320k to maximize MP3 quality and match common "highest quality"
		// expectations for lossy output.
		args = append(args,
			"-c:a", "libmp3lame",
			"-b:a", "320k",
		)
	case OutputFormatFLAC:
		args = append(args, "-c:a", "flac")
	default:
		args = append(args, "-c:a", "copy")
	}

	if muxer := outputMuxer(format, job.OutputPath, job.Metadata.FileExtension); muxer != "" {
		args = append(args, "-f", muxer)
	}

	if job.Tagging.Enabled {
		args = append(args, metadataArgs(job.Metadata)...)
	}

	if embedArtwork {
		switch format {
		case OutputFormatMP3:
			args = append(args,
				"-c:v", "mjpeg",
				"-id3v2_version", "3",
				"-metadata:s:v", "title=Album cover",
				"-metadata:s:v", "comment=Cover (front)",
			)
		case OutputFormatFLAC:
			args = append(args,
				"-c:v", "mjpeg",
				"-disposition:v", "attached_pic",
				"-metadata:s:v", "title=Album cover",
			)
		default:
			switch strings.ToLower(filepath.Ext(job.OutputPath)) {
			case ".mp3":
				args = append(args,
					"-c:v", "mjpeg",
					"-id3v2_version", "3",
					"-metadata:s:v", "title=Album cover",
					"-metadata:s:v", "comment=Cover (front)",
				)
			case ".flac":
				args = append(args,
					"-c:v", "mjpeg",
					"-disposition:v", "attached_pic",
					"-metadata:s:v", "title=Album cover",
				)
			}
		}
	}

	return append(args, job.OutputPath)
}

func outputMuxer(format OutputFormat, outputPath, sourceExt string) string {
	switch normalizeOutputFormat(format) {
	case OutputFormatMP3:
		return "mp3"
	case OutputFormatFLAC:
		return "flac"
	case OutputFormatSource:
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(outputPath)), ".")
		switch ext {
		case "ogg":
			return "ogg"
		case "mp3":
			return "mp3"
		case "aac":
			return "adts"
		case "flac":
			return "flac"
		}
		if ext == "" || ext == "part" {
			ext = strings.TrimPrefix(strings.ToLower(sourceExt), ".")
		}
		switch ext {
		case "ogg":
			return "ogg"
		case "mp3":
			return "mp3"
		case "aac":
			return "adts"
		case "flac":
			return "flac"
		default:
			return ""
		}
	default:
		return ""
	}
}

func runFFmpegPostProcess(ctx context.Context, ffmpegPath string, job ffmpegJob) error {
	args := buildFFmpegArgs(job)
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return fmt.Errorf("ffmpeg failed: %w: %s", err, message)
		}
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	return nil
}

func (d *Downloader) postProcessFile(ctx context.Context, rawPath, outputPath string, metadata MediaMetadata, req Request) error {
	format := normalizeOutputFormat(req.OutputFormat)
	if format == "" {
		return fmt.Errorf("unsupported output format %q", req.OutputFormat)
	}

	var artworkPath string
	artworkURL := metadata.ArtworkURL
	if req.Tagging.IncludeArtwork && artworkURL == "" {
		if fallbackURL, err := d.lookupArtworkURL(ctx, metadata.URI, metadata.Kind); err != nil {
			d.log.WithError(err).Warnf("failed resolving fallback artwork URL for %s", metadata.URI)
		} else if fallbackURL != "" {
			artworkURL = fallbackURL
		}
	}

	if req.Tagging.IncludeArtwork && artworkURL != "" && canEmbedArtwork(format, outputPath) {
		var err error
		artworkPath, err = d.downloadArtworkFile(ctx, artworkURL)
		if err != nil {
			d.log.WithError(err).Warnf("failed downloading artwork for %s", metadata.URI)
		} else {
			defer func() { _ = os.Remove(artworkPath) }()
		}
	}

	tempOutputPath := outputPath + ".ffmpeg.part"
	defer func() { _ = os.Remove(tempOutputPath) }()

	if err := runFFmpegPostProcess(ctx, d.ffmpegPath, ffmpegJob{
		SourcePath:   rawPath,
		OutputPath:   tempOutputPath,
		OutputFormat: format,
		Metadata:     metadata,
		Tagging:      req.Tagging,
		ArtworkPath:  artworkPath,
	}); err != nil {
		return err
	}

	if err := os.Rename(tempOutputPath, outputPath); err != nil {
		return fmt.Errorf("move post-processed file into place: %w", err)
	}
	return nil
}

func (d *Downloader) lookupArtworkURL(ctx context.Context, uri string, kind ItemKind) (string, error) {
	id, err := librespotIDFromURI(uri)
	if err != nil {
		return "", err
	}

	type spotifyImage struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}

	bestURL := func(images []spotifyImage) string {
		bestArea := -1
		best := ""
		for _, img := range images {
			url := strings.TrimSpace(img.URL)
			if url == "" {
				continue
			}
			area := img.Width * img.Height
			if area > bestArea {
				bestArea = area
				best = url
			}
		}
		if best == "" && len(images) > 0 {
			return strings.TrimSpace(images[0].URL)
		}
		return best
	}

	switch kind {
	case ItemKindTrack:
		var resp struct {
			Album struct {
				Images []spotifyImage `json:"images"`
			} `json:"album"`
		}
		apiResp, err := d.sess.WebApi(ctx, http.MethodGet, "/v1/tracks/"+id, nil, nil, nil)
		if err != nil {
			return "", err
		}
		defer apiResp.Body.Close()
		if apiResp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("spotify web api /v1/tracks/%s returned status %d", id, apiResp.StatusCode)
		}
		if err := json.NewDecoder(apiResp.Body).Decode(&resp); err != nil {
			return "", err
		}
		return bestURL(resp.Album.Images), nil
	case ItemKindEpisode:
		var resp struct {
			Images []spotifyImage `json:"images"`
		}
		apiResp, err := d.sess.WebApi(ctx, http.MethodGet, "/v1/episodes/"+id, nil, nil, nil)
		if err != nil {
			return "", err
		}
		defer apiResp.Body.Close()
		if apiResp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("spotify web api /v1/episodes/%s returned status %d", id, apiResp.StatusCode)
		}
		if err := json.NewDecoder(apiResp.Body).Decode(&resp); err != nil {
			return "", err
		}
		return bestURL(resp.Images), nil
	default:
		return "", nil
	}
}

func librespotIDFromURI(uri string) (string, error) {
	const prefix = "spotify:"
	if !strings.HasPrefix(uri, prefix) {
		return "", fmt.Errorf("invalid spotify URI %q", uri)
	}
	parts := strings.Split(uri, ":")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid spotify URI %q", uri)
	}
	id := strings.TrimSpace(parts[len(parts)-1])
	if id == "" {
		return "", fmt.Errorf("invalid spotify URI %q", uri)
	}
	return id, nil
}

func (d *Downloader) downloadArtworkFile(ctx context.Context, artworkURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artworkURL, nil)
	if err != nil {
		return "", fmt.Errorf("create artwork request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download artwork: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download artwork returned status %d", resp.StatusCode)
	}

	ext := ".img"
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil {
		switch mediaType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		}
	}

	file, err := os.CreateTemp("", "libspotdl-artwork-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create artwork temp file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("store artwork temp file: %w", err)
	}

	return file.Name(), nil
}
