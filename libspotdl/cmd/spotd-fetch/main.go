package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/McMelonTV/Voxora/libspotd"
)

func main() {
	var (
		outputDir    = flag.String("out", ".", "directory where downloaded files will be written")
		outputPath   = flag.String("output", "", "explicit output path for a single item")
		bitrate      = flag.Int("bitrate", 320, "preferred Spotify source bitrate")
		format       = flag.String("format", "mp3", "final output format: source, mp3, or flac")
		lossless     = flag.Bool("lossless", false, "encode output as FLAC to avoid additional lossy re-encoding")
		overwrite    = flag.Bool("overwrite", false, "overwrite existing files")
		tagging      = flag.Bool("tagging", true, "write metadata tags when outputting to a file")
		artwork      = flag.Bool("artwork", true, "embed album/show artwork when supported by the final output format")
		ffmpegPath   = flag.String("ffmpeg", "ffmpeg", "path to the ffmpeg executable used for tagging/transcoding")
		callbackPort = flag.Int("callback-port", 36842, "callback port for interactive Spotify auth")
		credsFile    = flag.String("credentials", "", "credential cache file (defaults to the user config dir)")
		ignoreCache  = flag.Bool("ignore-cache-auth", false, "ignore cached stored credentials and force token/interactive auth")
		resetAuth    = flag.Bool("reset-auth", false, "delete the cached credentials file before authenticating")
		username     = flag.String("username", "", "Spotify username to pair with -access-token for non-interactive auth")
		accessToken  = flag.String("access-token", "", "Spotify access token for non-interactive auth")
		deviceID     = flag.String("device-id", "", "optional fixed Spotify device ID to reuse")
	)
	flag.Parse()

	if flag.NArg() != 1 {
		_, _ = fmt.Fprintf(os.Stderr, "usage: %s [flags] <spotify-url-or-uri>\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(2)
	}

	outputFormat := libspotdl.OutputFormat(strings.ToLower(strings.TrimSpace(*format)))
	if *lossless {
		outputFormat = libspotdl.OutputFormatFLAC
	}
	source := flag.Arg(0)
	credentialsPath, err := libspotdl.ResolveCredentialsFile(*credsFile)
	if err != nil {
		log.Fatal(err)
	}
	if *resetAuth {
		if err := os.Remove(credentialsPath); err != nil && !os.IsNotExist(err) {
			log.Fatalf("remove credentials cache %s: %v", credentialsPath, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	downloader, err := libspotdl.New(ctx, libspotdl.Config{
		PreferredBitrate: *bitrate,
		FFmpegPath:       *ffmpegPath,
		Auth: libspotdl.AuthConfig{
			CallbackPort:            *callbackPort,
			CredentialsFile:         credentialsPath,
			IgnoreStoredCredentials: *ignoreCache,
			Username:                *username,
			AccessToken:             *accessToken,
			DeviceID:                *deviceID,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer downloader.Close()

	lastLineLen := 0
	results, err := downloader.Download(ctx, libspotdl.Request{
		Source:       source,
		OutputDir:    *outputDir,
		OutputPath:   *outputPath,
		Overwrite:    *overwrite,
		OutputFormat: outputFormat,
		Tagging: libspotdl.TaggingConfig{
			Enabled:        *tagging,
			IncludeArtwork: *artwork,
		},
		Progress: func(p libspotdl.Progress) {
			line := fmt.Sprintf("[%d/%d] %-11s %s", p.ItemIndex+1, max(p.ItemCount, 1), p.Stage, p.URI)
			if p.TotalBytes > 0 {
				line = fmt.Sprintf("%s (%d/%d bytes)", line, p.BytesWritten, p.TotalBytes)
			}
			if p.OutputPath != "" {
				line = fmt.Sprintf("%s -> %s", line, p.OutputPath)
			}
			if len(line) < lastLineLen {
				line += strings.Repeat(" ", lastLineLen-len(line))
			}
			lastLineLen = len(line)
			fmt.Printf("\r%s", line)
			if p.Stage == "completed" || p.Stage == "skipped" {
				fmt.Println()
			}
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("downloaded %d item(s)\n", len(results))
	for _, result := range results {
		status := "downloaded"
		if result.Skipped {
			status = "skipped"
		}
		fmt.Printf("- %s: %s [%s]\n", status, result.Metadata.DefaultBaseName, result.OutputFormat)
		if result.OutputPath != "" {
			fmt.Printf("  -> %s\n", result.OutputPath)
		}
	}
}
