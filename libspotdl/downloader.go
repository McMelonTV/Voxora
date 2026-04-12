package libspotdl

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/ap"
	"github.com/devgianlu/go-librespot/audio"
	storagepb "github.com/devgianlu/go-librespot/proto/spotify/download"
	extmetadatapb "github.com/devgianlu/go-librespot/proto/spotify/extendedmetadata"
	audiofilespb "github.com/devgianlu/go-librespot/proto/spotify/extendedmetadata/audiofiles"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
	librespotsession "github.com/devgianlu/go-librespot/session"
)

type Downloader struct {
	sess             *librespotsession.Session
	log              librespot.Logger
	client           *http.Client
	preferredBitrate int
	countryCode      string
	ffmpegPath       string
}

type selectedMedia struct {
	id       librespot.SpotifyId
	metadata MediaMetadata
	file     *metadatapb.AudioFile
	files    []*metadatapb.AudioFile
}

type rawReadCloser struct {
	reader io.Reader
	closer io.Closer
}

func (r *rawReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *rawReadCloser) Close() error {
	if r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

func New(ctx context.Context, cfg Config) (*Downloader, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	log := cfg.Logger
	if log == nil {
		log = newDefaultLogger()
	}

	preferredBitrate := cfg.PreferredBitrate
	if preferredBitrate <= 0 {
		preferredBitrate = 320
	}

	ffmpegPath := strings.TrimSpace(cfg.FFmpegPath)
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	sess, err := newAuthenticatedSession(ctx, cfg, log, client)
	if err != nil {
		return nil, err
	}

	d := &Downloader{
		sess:             sess,
		log:              log,
		client:           client,
		preferredBitrate: preferredBitrate,
		ffmpegPath:       ffmpegPath,
	}
	if country, err := d.detectCountryCode(ctx); err == nil {
		d.countryCode = country
	}
	d.warmUpAccesspoint(ctx)

	return d, nil
}

func (d *Downloader) Close() error {
	if d == nil || d.sess == nil {
		return nil
	}
	d.sess.Close()
	return nil
}

func (d *Downloader) warmUpAccesspoint(ctx context.Context) {
	if d == nil || d.sess == nil {
		return
	}

	ch := d.sess.Accesspoint().Receive(
		ap.PacketTypeSecretBlock,
		ap.PacketTypeLicenseVersion,
		ap.PacketTypeCountryCode,
		ap.PacketTypeProductInfo,
		ap.PacketTypeUnknown1f,
		ap.PacketTypeLegacyWelcome,
		ap.PacketTypeMercuryEvent,
	)

	deadline := time.NewTimer(2 * time.Second)
	idle := time.NewTimer(250 * time.Millisecond)
	defer deadline.Stop()
	defer idle.Stop()

	drained := 0
	for {
		select {
		case <-ctx.Done():
			if drained > 0 {
				d.log.Debugf("drained %d queued accesspoint packets during warm-up", drained)
			}
			return
		case <-deadline.C:
			if drained > 0 {
				d.log.Debugf("drained %d queued accesspoint packets during warm-up", drained)
			}
			return
		case <-idle.C:
			if drained > 0 {
				d.log.Debugf("drained %d queued accesspoint packets during warm-up", drained)
			}
			return
		case _, ok := <-ch:
			if !ok {
				if drained > 0 {
					d.log.Debugf("drained %d queued accesspoint packets during warm-up", drained)
				}
				return
			}
			drained++
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(250 * time.Millisecond)
		}
	}
}

func (d *Downloader) Download(ctx context.Context, req Request) ([]Result, error) {
	if normalizeOutputFormat(req.OutputFormat) == "" {
		return nil, fmt.Errorf("unsupported output format %q", req.OutputFormat)
	}

	items, err := d.Resolve(ctx, req.Source)
	if err != nil {
		return nil, err
	}

	if req.Writer != nil {
		if len(items) != 1 {
			return nil, errors.New("writer output is only supported for a single resolved item")
		}
		if needsPostProcess(req) {
			return nil, errors.New("writer output only supports the raw downloaded Spotify source; use file output for tagging or transcoding")
		}
	}
	if req.OutputPath != "" && len(items) != 1 {
		return nil, errors.New("explicit output path is only supported for a single resolved item")
	}

	results := make([]Result, 0, len(items))
	for i, item := range items {
		writer := req.Writer
		if i > 0 {
			writer = nil
		}

		result, err := d.downloadOne(ctx, item, len(items), Request{
			Source:       req.Source,
			OutputDir:    req.OutputDir,
			OutputPath:   req.OutputPath,
			Writer:       writer,
			Overwrite:    req.Overwrite,
			OutputFormat: req.OutputFormat,
			Tagging:      req.Tagging,
			Progress:     req.Progress,
		})
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}

	return results, nil
}

// StreamTrack writes one track's raw source stream sequentially to all provided
// writers, preserving byte order for live playback pipelines.
func (d *Downloader) StreamTrack(ctx context.Context, uri string, writers []io.Writer, progress func(Progress)) (MediaMetadata, int64, error) {
	if strings.TrimSpace(uri) == "" {
		return MediaMetadata{}, 0, errors.New("empty track uri")
	}
	if len(writers) == 0 {
		return MediaMetadata{}, 0, errors.New("no stream writers provided")
	}

	id, err := librespot.SpotifyIdFromUri(uri)
	if err != nil {
		return MediaMetadata{}, 0, fmt.Errorf("parse track uri %s: %w", uri, err)
	}

	selected, err := d.prepareDownload(ctx, *id, ItemKindTrack)
	if err != nil {
		return MediaMetadata{}, 0, fmt.Errorf("prepare %s: %w", uri, err)
	}
	selected.metadata.URI = uri

	stream, totalBytes, err := d.openRawStream(ctx, selected)
	if err != nil {
		return MediaMetadata{}, 0, fmt.Errorf("open raw stream: %w", err)
	}
	defer stream.Close()

	emitProgress(progress, Progress{
		URI:        uri,
		Kind:       ItemKindTrack,
		ItemIndex:  0,
		ItemCount:  1,
		Stage:      "downloading",
		TotalBytes: totalBytes,
	})

	multi := io.MultiWriter(writers...)
	written, err := copyWithProgress(ctx, multi, stream, func(written int64) {
		emitProgress(progress, Progress{
			URI:          uri,
			Kind:         ItemKindTrack,
			ItemIndex:    0,
			ItemCount:    1,
			Stage:        "downloading",
			BytesWritten: written,
			TotalBytes:   totalBytes,
		})
	})
	if err != nil {
		return MediaMetadata{}, written, err
	}

	emitProgress(progress, Progress{
		URI:          uri,
		Kind:         ItemKindTrack,
		ItemIndex:    0,
		ItemCount:    1,
		Stage:        "completed",
		BytesWritten: written,
		TotalBytes:   totalBytes,
	})

	return selected.metadata, written, nil
}

func (d *Downloader) downloadOne(ctx context.Context, item ResolvedItem, itemCount int, req Request) (Result, error) {
	id, err := librespot.SpotifyIdFromUri(item.URI)
	if err != nil {
		return Result{}, fmt.Errorf("parse resolved uri %s: %w", item.URI, err)
	}

	selected, err := d.prepareDownload(ctx, *id, item.Kind)
	if err != nil {
		return Result{}, fmt.Errorf("prepare %s: %w", item.URI, err)
	}
	// Preserve the requested URI in metadata/comments even when playback is
	// served via an internally relinked track ID.
	selected.metadata.URI = item.URI

	outputPath, err := resolveOutputPath(req, selected.metadata, itemCount)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		URI:          item.URI,
		Kind:         item.Kind,
		OutputPath:   outputPath,
		OutputFormat: normalizeOutputFormat(req.OutputFormat),
		Metadata:     selected.metadata,
	}
	if result.OutputFormat == "" {
		result.OutputFormat = OutputFormatSource
	}

	if req.Writer == nil && outputPath != "" && !req.Overwrite {
		if _, err := os.Stat(outputPath); err == nil {
			result.Skipped = true
			emitProgress(req.Progress, Progress{
				URI:        item.URI,
				Kind:       item.Kind,
				ItemIndex:  item.Index,
				ItemCount:  itemCount,
				Stage:      "skipped",
				OutputPath: outputPath,
			})
			return result, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("check output path: %w", err)
		}
	}

	stream, totalBytes, err := d.openRawStream(ctx, selected)
	if err != nil {
		return Result{}, fmt.Errorf("open raw stream: %w", err)
	}
	defer stream.Close()

	emitProgress(req.Progress, Progress{
		URI:        item.URI,
		Kind:       item.Kind,
		ItemIndex:  item.Index,
		ItemCount:  itemCount,
		Stage:      "downloading",
		TotalBytes: totalBytes,
		OutputPath: outputPath,
	})

	if req.Writer != nil {
		written, err := copyWithProgress(ctx, req.Writer, stream, func(written int64) {
			emitProgress(req.Progress, Progress{
				URI:          item.URI,
				Kind:         item.Kind,
				ItemIndex:    item.Index,
				ItemCount:    itemCount,
				Stage:        "downloading",
				BytesWritten: written,
				TotalBytes:   totalBytes,
			})
		})
		if err != nil {
			return Result{}, err
		}
		result.BytesWritten = written
		emitProgress(req.Progress, Progress{
			URI:          item.URI,
			Kind:         item.Kind,
			ItemIndex:    item.Index,
			ItemCount:    itemCount,
			Stage:        "completed",
			BytesWritten: written,
			TotalBytes:   totalBytes,
		})
		return result, nil
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("create output dir: %w", err)
	}

	rawTempPath := tempSourcePath(outputPath, selected.metadata)
	rawFile, err := os.Create(rawTempPath)
	if err != nil {
		return Result{}, fmt.Errorf("create temporary source file: %w", err)
	}

	written, copyErr := copyWithProgress(ctx, rawFile, stream, func(written int64) {
		emitProgress(req.Progress, Progress{
			URI:          item.URI,
			Kind:         item.Kind,
			ItemIndex:    item.Index,
			ItemCount:    itemCount,
			Stage:        "downloading",
			BytesWritten: written,
			TotalBytes:   totalBytes,
			OutputPath:   outputPath,
		})
	})
	closeErr := rawFile.Close()
	if copyErr != nil {
		_ = os.Remove(rawTempPath)
		return Result{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(rawTempPath)
		return Result{}, fmt.Errorf("close temporary source file: %w", closeErr)
	}

	if !needsPostProcess(req) {
		if err := os.Rename(rawTempPath, outputPath); err != nil {
			_ = os.Remove(rawTempPath)
			return Result{}, fmt.Errorf("move output file into place: %w", err)
		}
		result.BytesWritten = written
		emitProgress(req.Progress, Progress{
			URI:          item.URI,
			Kind:         item.Kind,
			ItemIndex:    item.Index,
			ItemCount:    itemCount,
			Stage:        "completed",
			BytesWritten: written,
			TotalBytes:   totalBytes,
			OutputPath:   outputPath,
		})
		return result, nil
	}
	defer os.Remove(rawTempPath)

	stage := "tagging"
	if normalizeOutputFormat(req.OutputFormat) != OutputFormatSource {
		stage = "transcoding"
	}
	emitProgress(req.Progress, Progress{
		URI:        item.URI,
		Kind:       item.Kind,
		ItemIndex:  item.Index,
		ItemCount:  itemCount,
		Stage:      stage,
		OutputPath: outputPath,
	})

	if err := d.postProcessFile(ctx, rawTempPath, outputPath, selected.metadata, req); err != nil {
		return Result{}, fmt.Errorf("post-process %s: %w", item.URI, err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat final output file: %w", err)
	}
	result.BytesWritten = info.Size()

	emitProgress(req.Progress, Progress{
		URI:          item.URI,
		Kind:         item.Kind,
		ItemIndex:    item.Index,
		ItemCount:    itemCount,
		Stage:        "completed",
		BytesWritten: result.BytesWritten,
		OutputPath:   outputPath,
	})
	return result, nil
}

func emitProgress(fn func(Progress), progress Progress) {
	if fn != nil {
		fn(progress)
	}
}

func resolveOutputPath(req Request, metadata MediaMetadata, itemCount int) (string, error) {
	if req.Writer != nil {
		return "", nil
	}

	ext := outputExtension(req.OutputFormat, metadata)
	if ext == "" {
		return "", errors.New("could not determine output extension")
	}

	if req.OutputPath != "" {
		if itemCount != 1 {
			return "", errors.New("output path can only be used with a single item")
		}
		if filepath.Ext(req.OutputPath) == "" {
			return req.OutputPath + "." + ext, nil
		}
		return req.OutputPath, nil
	}

	outputDir := req.OutputDir
	if strings.TrimSpace(outputDir) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		outputDir = cwd
	}

	return filepath.Join(outputDir, metadata.DefaultBaseName+"."+ext), nil
}

func tempSourcePath(outputPath string, metadata MediaMetadata) string {
	base := strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath))
	return filepath.Join(filepath.Dir(outputPath), "."+base+".source."+metadata.FileExtension+".part")
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, onProgress func(int64)) (int64, error) {
	buf := make([]byte, 128*1024)
	var written int64

	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}

		n, readErr := src.Read(buf)
		if n > 0 {
			wn, writeErr := dst.Write(buf[:n])
			written += int64(wn)
			if onProgress != nil {
				onProgress(written)
			}
			if writeErr != nil {
				return written, fmt.Errorf("write output: %w", writeErr)
			}
			if wn != n {
				return written, io.ErrShortWrite
			}
		}

		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, fmt.Errorf("read spotify stream: %w", readErr)
		}
	}
}

func (d *Downloader) prepareDownload(ctx context.Context, id librespot.SpotifyId, kind ItemKind) (*selectedMedia, error) {
	switch kind {
	case ItemKindTrack:
		trackMeta, actualID, err := d.resolveTrackMetadata(ctx, id)
		if err != nil {
			return nil, err
		}

		var audioFilesResp audiofilespb.AudioFilesExtensionResponse
		if err := d.sess.Spclient().ExtendedMetadataSimple(ctx, actualID, extmetadatapb.ExtensionKind_AUDIO_FILES, &audioFilesResp); err != nil {
			return nil, fmt.Errorf("get track audio files: %w", err)
		}

		files := collectTrackAudioCandidates(trackMeta, &audioFilesResp)
		if len(files) == 0 {
			return nil, librespot.ErrNoSupportedFormats
		}
		files = sortAudioFilesByPreference(files, d.preferredBitrate)

		selected := selectBestAudioFile(files, d.preferredBitrate)
		if selected == nil {
			return nil, librespot.ErrNoSupportedFormats
		}

		metadata := buildTrackMetadata(actualID, trackMeta, selected.GetFormat(), d.preferredBitrate)
		if metadata.ArtworkURL == "" {
			if fallback := d.resolveTrackAlbumArtworkURL(ctx, trackMeta); fallback != "" {
				metadata.ArtworkURL = fallback
			}
		}
		return &selectedMedia{id: actualID, metadata: metadata, file: selected, files: files}, nil
	case ItemKindEpisode:
		var episodeMeta metadatapb.Episode
		if err := d.sess.Spclient().ExtendedMetadataSimple(ctx, id, extmetadatapb.ExtensionKind_EPISODE_V4, &episodeMeta); err != nil {
			return nil, fmt.Errorf("get episode metadata: %w", err)
		}

		if isMediaRestricted(librespot.NewMediaFromEpisode(&episodeMeta), d.countryCode) {
			return nil, librespot.ErrMediaRestricted
		}

		files := sortAudioFilesByPreference(dedupeAudioFiles(episodeMeta.GetAudio()), d.preferredBitrate)
		selected := selectBestAudioFile(files, d.preferredBitrate)
		if selected == nil {
			return nil, librespot.ErrNoSupportedFormats
		}

		metadata := buildEpisodeMetadata(id, &episodeMeta, selected.GetFormat(), d.preferredBitrate)
		return &selectedMedia{id: id, metadata: metadata, file: selected, files: files}, nil
	default:
		return nil, fmt.Errorf("unsupported item kind for download: %s", kind)
	}
}

func (d *Downloader) resolveTrackAlbumArtworkURL(ctx context.Context, track *metadatapb.Track) string {
	if track == nil || track.GetAlbum() == nil {
		return ""
	}

	if url := artworkURLFromImages(track.GetAlbum().GetCover()); url != "" {
		return url
	}

	albumGID := track.GetAlbum().GetGid()
	if len(albumGID) == 0 {
		return ""
	}

	albumID := librespot.SpotifyIdFromGid(librespot.SpotifyIdType("album"), albumGID)
	var albumMeta metadatapb.Album
	if err := d.sess.Spclient().ExtendedMetadataSimple(ctx, albumID, extmetadatapb.ExtensionKind_ALBUM_V4, &albumMeta); err != nil {
		d.log.WithError(err).Warnf("failed loading album metadata for artwork fallback %s", albumID.Uri())
		return ""
	}

	if url := artworkURLFromImages(albumMeta.GetCoverGroup().GetImage()); url != "" {
		return url
	}
	return artworkURLFromImages(albumMeta.GetCover())
}

func (d *Downloader) resolveTrackMetadata(ctx context.Context, id librespot.SpotifyId) (*metadatapb.Track, librespot.SpotifyId, error) {
	var trackMeta metadatapb.Track
	if err := d.sess.Spclient().ExtendedMetadataSimple(ctx, id, extmetadatapb.ExtensionKind_TRACK_V4, &trackMeta); err != nil {
		return nil, librespot.SpotifyId{}, fmt.Errorf("get track metadata: %w", err)
	}

	media := librespot.NewMediaFromTrack(&trackMeta)
	if !isMediaRestricted(media, d.countryCode) && len(trackMeta.GetFile()) > 0 {
		return &trackMeta, media.Id(), nil
	}

	for _, alternative := range trackMeta.GetAlternative() {
		altMedia := librespot.NewMediaFromTrack(alternative)
		if isMediaRestricted(altMedia, d.countryCode) || len(alternative.GetFile()) == 0 {
			continue
		}

		trackMeta.Alternative = nil
		trackMeta.Gid = alternative.Gid
		trackMeta.File = alternative.File
		trackMeta.Preview = alternative.Preview
		trackMeta.OriginalAudio = alternative.OriginalAudio
		if trackMeta.GetAlbum() == nil {
			trackMeta.Album = alternative.Album
		} else if alternative.GetAlbum() != nil {
			if len(trackMeta.GetAlbum().GetCover()) == 0 && len(alternative.GetAlbum().GetCover()) > 0 {
				trackMeta.Album.Cover = alternative.GetAlbum().GetCover()
			}
		}
		return &trackMeta, altMedia.Id(), nil
	}

	if !isMediaRestricted(media, d.countryCode) {
		// Keep the original ID even when file list is empty; some releases provide
		// usable formats only via AUDIO_FILES extension for the same track ID.
		return &trackMeta, media.Id(), nil
	}

	return nil, librespot.SpotifyId{}, librespot.ErrMediaRestricted
}

func (d *Downloader) openRawStream(ctx context.Context, selected *selectedMedia) (io.ReadCloser, int64, error) {
	if selected.file == nil || selected.file.Format == nil || len(selected.file.GetFileId()) == 0 {
		return nil, 0, errors.New("selected spotify audio file is incomplete")
	}

	candidates := selected.files
	if len(candidates) == 0 {
		candidates = []*metadatapb.AudioFile{selected.file}
	}

	var lastErr error
	for i, file := range candidates {
		if file == nil || file.Format == nil || len(file.GetFileId()) == 0 {
			continue
		}

		d.log.Debugf("opening raw stream for gid %s with file %s (%s) [candidate %d/%d]", librespot.GidToBase62(selected.id.Id()), hex.EncodeToString(file.GetFileId()), file.GetFormat().String(), i+1, len(candidates))

		storageResolve, err := d.sess.Spclient().ResolveStorageInteractive(ctx, file.GetFileId(), file.Format, false)
		if err != nil {
			lastErr = fmt.Errorf("resolve storage: %w", err)
			d.log.WithError(err).Warnf("failed to resolve storage for candidate file %s", hex.EncodeToString(file.GetFileId()))
			continue
		}

		rawStream, err := d.httpChunkedReaderFromStorageResolve(storageResolve)
		if err != nil {
			lastErr = err
			d.log.WithError(err).Warnf("failed to open CDN stream for candidate file %s", hex.EncodeToString(file.GetFileId()))
			continue
		}

		audioKey, err := d.requestAudioKey(ctx, selected.id.Id(), file.GetFileId())
		if err != nil {
			if canUseRawStreamWithoutAudioKey(file.GetFormat(), rawStream) {
				d.log.WithError(err).Warnf("audio key unavailable, continuing without decryptor because stream appears to be plain %s", file.GetFormat().String())
				return d.prepareReadableStream(file.GetFormat(), rawStream, rawStream, rawStream.Size())
			}

			_ = rawStream.Close()
			lastErr = fmt.Errorf("retrieve audio key: %w", err)
			d.log.WithError(err).Warnf("audio key request failed for candidate file %s", hex.EncodeToString(file.GetFileId()))
			continue
		}

		decryptedStream, err := audio.NewAesAudioDecryptor(rawStream, audioKey)
		if err != nil {
			_ = rawStream.Close()
			lastErr = fmt.Errorf("initialize decryptor: %w", err)
			d.log.WithError(err).Warnf("failed to initialize decryptor for candidate file %s", hex.EncodeToString(file.GetFileId()))
			continue
		}

		return d.prepareReadableStream(file.GetFormat(), decryptedStream, decryptedStream, rawStream.Size())
	}

	if lastErr == nil {
		lastErr = errors.New("no usable spotify audio file candidates")
	}
	return nil, 0, withAudioKeyTroubleshooting(lastErr)
}

func (d *Downloader) prepareReadableStream(format metadatapb.AudioFile_Format, reader io.ReaderAt, closer io.Closer, totalBytes int64) (io.ReadCloser, int64, error) {
	offset := int64(0)
	if isOggFormat(format) {
		offset = detectOggPayloadOffset(reader, totalBytes)
		if offset > 0 {
			d.log.Debugf("detected Spotify OGG payload offset %d bytes", offset)
		}
	}

	if offset > totalBytes {
		return nil, 0, fmt.Errorf("invalid stream offset %d for stream size %d", offset, totalBytes)
	}

	readableBytes := totalBytes - offset
	section := io.NewSectionReader(reader, offset, readableBytes)
	return &rawReadCloser{reader: section, closer: closer}, readableBytes, nil
}

func isOggFormat(format metadatapb.AudioFile_Format) bool {
	switch format {
	case metadatapb.AudioFile_OGG_VORBIS_96, metadatapb.AudioFile_OGG_VORBIS_160, metadatapb.AudioFile_OGG_VORBIS_320:
		return true
	default:
		return false
	}
}

func detectOggPayloadOffset(reader io.ReaderAt, totalBytes int64) int64 {
	if reader == nil || totalBytes <= 0 {
		return 0
	}

	const spotifyOggHeaderOffset = int64(0xA7)
	// Spotify can prepend a custom 0xA7-byte header before the real OGG payload.
	// Some streams start with bytes that include "OggS" at offset 0 but are still
	// not a valid decodable OGG stream, so prefer the known Spotify payload offset
	// whenever it also contains a valid Ogg page marker.
	if spotifyOggHeaderOffset+4 <= totalBytes && hasMagic(reader, spotifyOggHeaderOffset, []byte("OggS")) {
		return spotifyOggHeaderOffset
	}
	if hasMagic(reader, 0, []byte("OggS")) {
		return 0
	}
	return 0
}

func hasMagic(reader io.ReaderAt, offset int64, magic []byte) bool {
	if len(magic) == 0 {
		return false
	}
	buf := make([]byte, len(magic))
	n, err := reader.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	return n == len(magic) && bytes.Equal(buf, magic)
}

func canUseRawStreamWithoutAudioKey(format metadatapb.AudioFile_Format, stream *audio.HttpChunkedReader) bool {
	if stream == nil {
		return false
	}

	readHeader := func(offset int64) ([]byte, int, error) {
		header := make([]byte, 4)
		n, err := stream.ReadAt(header, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, err
		}
		return header, n, nil
	}

	switch format {
	case metadatapb.AudioFile_OGG_VORBIS_96, metadatapb.AudioFile_OGG_VORBIS_160, metadatapb.AudioFile_OGG_VORBIS_320:
		// Spotify OGG often includes a 0xA7-byte custom header before the OggS stream.
		header, n, err := readHeader(0xA7)
		if err == nil && n >= 4 && bytes.Equal(header[:4], []byte("OggS")) {
			return true
		}
		header, n, err = readHeader(0)
		return err == nil && n >= 4 && bytes.Equal(header[:4], []byte("OggS"))
	case metadatapb.AudioFile_MP3_96, metadatapb.AudioFile_MP3_160, metadatapb.AudioFile_MP3_160_ENC, metadatapb.AudioFile_MP3_256, metadatapb.AudioFile_MP3_320:
		header, n, err := readHeader(0)
		if err != nil || n < 2 {
			return false
		}
		if n >= 3 && bytes.Equal(header[:3], []byte("ID3")) {
			return true
		}
		return header[0] == 0xff && (header[1]&0xe0) == 0xe0
	case metadatapb.AudioFile_AAC_24, metadatapb.AudioFile_AAC_48, metadatapb.AudioFile_XHE_AAC_12, metadatapb.AudioFile_XHE_AAC_16, metadatapb.AudioFile_XHE_AAC_24:
		header, n, err := readHeader(0)
		if err != nil || n < 2 {
			return false
		}
		return header[0] == 0xff && (header[1]&0xf0) == 0xf0
	default:
		return false
	}
}

func (d *Downloader) requestAudioKey(ctx context.Context, gid []byte, fileID []byte) ([]byte, error) {
	return requestAudioKeyWithRetry(ctx, d.log, func(ctx context.Context) ([]byte, error) {
		return d.sess.AudioKey().Request(ctx, gid, fileID)
	})
}

func requestAudioKeyWithRetry(ctx context.Context, log librespot.Logger, request func(context.Context) ([]byte, error)) ([]byte, error) {
	const maxAttempts = 3
	const retryDelay = 150 * time.Millisecond

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		audioKey, err := request(ctx)
		if err == nil {
			return audioKey, nil
		}

		lastErr = err

		var keyErr *audio.KeyProviderError
		if !errors.As(err, &keyErr) || keyErr.Code != 1 || attempt == maxAttempts {
			return nil, err
		}

		log.WithField("attempt", attempt).Warnf("received aes key error code 1, retrying request")

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return nil, lastErr
}

func (d *Downloader) httpChunkedReaderFromStorageResolve(storageResolve *storagepb.StorageResolveResponse) (*audio.HttpChunkedReader, error) {
	switch storageResolve.Result {
	case storagepb.StorageResolveResponse_STORAGE:
		return nil, errors.New("legacy spotify storage is not supported")
	case storagepb.StorageResolveResponse_RESTRICTED:
		return nil, errors.New("spotify storage is restricted")
	case storagepb.StorageResolveResponse_CDN:
		if len(storageResolve.Cdnurl) == 0 {
			return nil, errors.New("spotify storage resolve returned no CDN URLs")
		}

		var lastErr error
		for _, rawURL := range storageResolve.Cdnurl {
			parsed, err := url.Parse(rawURL)
			if err != nil {
				lastErr = fmt.Errorf("parse cdn url: %w", err)
				continue
			}

			reader, err := audio.NewHttpChunkedReader(d.log.WithField("host", parsed.Host), d.client, parsed.String())
			if err == nil {
				return reader, nil
			}
			lastErr = err
		}

		if lastErr == nil {
			lastErr = errors.New("spotify cdn download setup failed")
		}
		return nil, fmt.Errorf("open spotify cdn stream: %w", lastErr)
	default:
		return nil, fmt.Errorf("unknown storage resolve result: %s", storageResolve.Result)
	}
}

func (d *Downloader) detectCountryCode(ctx context.Context) (string, error) {
	resp, err := d.sess.WebApi(ctx, http.MethodGet, "/v1/me", nil, nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("spotify web api /v1/me returned status %d", resp.StatusCode)
	}

	var profile struct {
		Country string `json:"country"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return "", fmt.Errorf("decode /v1/me response: %w", err)
	}

	return strings.ToUpper(strings.TrimSpace(profile.Country)), nil
}

func buildTrackMetadata(id librespot.SpotifyId, track *metadatapb.Track, format metadatapb.AudioFile_Format, preferredBitrate int) MediaMetadata {
	artists := make([]string, 0, len(track.GetArtist()))
	for _, artist := range track.GetArtist() {
		if name := strings.TrimSpace(artist.GetName()); name != "" {
			artists = append(artists, name)
		}
	}

	album := ""
	var releaseDate string
	var artworkURL string
	if track.GetAlbum() != nil {
		album = strings.TrimSpace(track.GetAlbum().GetName())
		releaseDate = formatMetadataDate(track.GetAlbum().GetDate())
		artworkURL = artworkURLFromImages(track.GetAlbum().GetCover())
	}

	baseName := trackBaseName(artists, track.GetName())
	return MediaMetadata{
		URI:              id.Uri(),
		Kind:             ItemKindTrack,
		Title:            track.GetName(),
		Artists:          artists,
		Album:            album,
		ReleaseDate:      releaseDate,
		TrackNumber:      int(track.GetNumber()),
		DiscNumber:       int(track.GetDiscNumber()),
		ArtworkURL:       artworkURL,
		Duration:         time.Duration(track.GetDuration()) * time.Millisecond,
		SourceFormat:     format.String(),
		FileExtension:    extensionForFormat(format),
		ContentType:      contentTypeForFormat(format),
		DefaultBaseName:  baseName,
		DefaultFileName:  baseName + "." + extensionForFormat(format),
		PreferredBitrate: preferredBitrate,
	}
}

func buildEpisodeMetadata(id librespot.SpotifyId, episode *metadatapb.Episode, format metadatapb.AudioFile_Format, preferredBitrate int) MediaMetadata {
	show := ""
	if episode.GetShow() != nil {
		show = strings.TrimSpace(episode.GetShow().GetName())
	}

	artists := []string{}
	if show != "" {
		artists = append(artists, show)
	}

	baseName := episodeBaseName(show, episode.GetName())
	return MediaMetadata{
		URI:              id.Uri(),
		Kind:             ItemKindEpisode,
		Title:            episode.GetName(),
		Artists:          artists,
		Album:            show,
		Show:             show,
		ReleaseDate:      formatMetadataDate(episode.GetPublishTime()),
		TrackNumber:      int(episode.GetNumber()),
		ArtworkURL:       artworkURLFromImages(episode.GetCoverImage().GetImage()),
		Duration:         time.Duration(episode.GetDuration()) * time.Millisecond,
		SourceFormat:     format.String(),
		FileExtension:    extensionForFormat(format),
		ContentType:      contentTypeForFormat(format),
		DefaultBaseName:  baseName,
		DefaultFileName:  baseName + "." + extensionForFormat(format),
		PreferredBitrate: preferredBitrate,
	}
}

func formatMetadataDate(date *metadatapb.Date) string {
	if date == nil || date.GetYear() == 0 {
		return ""
	}
	if date.GetMonth() == 0 {
		return fmt.Sprintf("%04d", date.GetYear())
	}
	if date.GetDay() == 0 {
		return fmt.Sprintf("%04d-%02d", date.GetYear(), date.GetMonth())
	}
	return fmt.Sprintf("%04d-%02d-%02d", date.GetYear(), date.GetMonth(), date.GetDay())
}

func artworkURLFromImages(images []*metadatapb.Image) string {
	imageID := bestImageID(images)
	if len(imageID) == 0 {
		return ""
	}
	return "https://i.scdn.co/image/" + hex.EncodeToString(imageID)
}

func bestImageID(images []*metadatapb.Image) []byte {
	if len(images) == 0 {
		return nil
	}

	var best *metadatapb.Image
	for _, img := range images {
		if img == nil {
			continue
		}
		if best == nil || imageArea(img) > imageArea(best) {
			best = img
		}
	}
	if best == nil {
		return nil
	}
	return best.GetFileId()
}

func imageArea(img *metadatapb.Image) int64 {
	if img == nil {
		return 0
	}
	if img.GetWidth() > 0 && img.GetHeight() > 0 {
		return int64(img.GetWidth()) * int64(img.GetHeight())
	}
	switch img.GetSize() {
	case metadatapb.Image_SMALL:
		return 1
	case metadatapb.Image_LARGE:
		return 3
	case metadatapb.Image_XLARGE:
		return 4
	default:
		return 2
	}
}

func trackBaseName(artists []string, title string) string {
	title = strings.TrimSpace(title)
	if len(artists) > 3 {
		return cleanFileName(fmt.Sprintf("%s, and others - %s", strings.Join(artists[:3], ", "), title))
	}
	if len(artists) == 0 {
		return cleanFileName(title)
	}
	return cleanFileName(fmt.Sprintf("%s - %s", strings.Join(artists, ", "), title))
}

func episodeBaseName(showName, title string) string {
	showName = strings.TrimSpace(showName)
	title = strings.TrimSpace(title)
	if showName == "" {
		return cleanFileName(title)
	}
	return cleanFileName(fmt.Sprintf("%s - %s", showName, title))
}

func cleanFileName(name string) string {
	invalidChars := map[rune]struct{}{
		'<':  {},
		'>':  {},
		':':  {},
		'\'': {},
		'"':  {},
		'/':  {},
		'\\': {},
		'|':  {},
		'?':  {},
		'*':  {},
		'.':  {},
	}

	var builder strings.Builder
	for _, r := range name {
		if _, invalid := invalidChars[r]; invalid {
			continue
		}
		if r < 32 {
			continue
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}

func selectBestAudioFile(files []*metadatapb.AudioFile, preferredBitrate int) *metadatapb.AudioFile {
	var best *metadatapb.AudioFile
	bestScore := int(^uint(0) >> 1)
	bestRank := int(^uint(0) >> 1)
	bestBitrate := -1

	for _, file := range files {
		if file == nil || file.Format == nil || len(file.GetFileId()) == 0 {
			continue
		}
		if !isSupportedSourceFormat(file.GetFormat()) {
			continue
		}

		bitrate := bitrateForFormat(file.GetFormat())
		score := absInt(preferredBitrate - bitrate)
		rank := formatRank(file.GetFormat())
		if best == nil || score < bestScore || (score == bestScore && rank < bestRank) || (score == bestScore && rank == bestRank && bitrate > bestBitrate) {
			best = file
			bestScore = score
			bestRank = rank
			bestBitrate = bitrate
		}
	}

	return best
}

func sortAudioFilesByPreference(files []*metadatapb.AudioFile, preferredBitrate int) []*metadatapb.AudioFile {
	files = dedupeAudioFiles(files)
	sort.SliceStable(files, func(i, j int) bool {
		fi, fj := files[i], files[j]
		bi := bitrateForFormat(fi.GetFormat())
		bj := bitrateForFormat(fj.GetFormat())
		di := absInt(preferredBitrate - bi)
		dj := absInt(preferredBitrate - bj)
		if di != dj {
			return di < dj
		}

		ri := formatRank(fi.GetFormat())
		rj := formatRank(fj.GetFormat())
		if ri != rj {
			return ri < rj
		}
		return bi > bj
	})
	return files
}

func dedupeAudioFiles(files []*metadatapb.AudioFile) []*metadatapb.AudioFile {
	out := make([]*metadatapb.AudioFile, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file == nil || file.Format == nil || len(file.GetFileId()) == 0 {
			continue
		}
		if !isSupportedSourceFormat(file.GetFormat()) {
			continue
		}
		key := fmt.Sprintf("%s:%d", hex.EncodeToString(file.GetFileId()), file.GetFormat())
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, file)
	}
	return out
}

func collectTrackAudioCandidates(trackMeta *metadatapb.Track, audioFilesResp *audiofilespb.AudioFilesExtensionResponse) []*metadatapb.AudioFile {
	files := make([]*metadatapb.AudioFile, 0, len(trackMeta.GetFile())+len(audioFilesResp.GetFiles()))
	files = append(files, trackMeta.GetFile()...)
	for _, file := range audioFilesResp.GetFiles() {
		if file != nil && file.File != nil {
			files = append(files, file.File)
		}
	}
	return dedupeAudioFiles(files)
}

func isSupportedSourceFormat(format metadatapb.AudioFile_Format) bool {
	switch format {
	case metadatapb.AudioFile_OGG_VORBIS_96,
		metadatapb.AudioFile_OGG_VORBIS_160,
		metadatapb.AudioFile_OGG_VORBIS_320,
		metadatapb.AudioFile_MP3_96,
		metadatapb.AudioFile_MP3_160,
		metadatapb.AudioFile_MP3_160_ENC,
		metadatapb.AudioFile_MP3_256,
		metadatapb.AudioFile_MP3_320,
		metadatapb.AudioFile_AAC_24,
		metadatapb.AudioFile_AAC_48,
		metadatapb.AudioFile_XHE_AAC_12,
		metadatapb.AudioFile_XHE_AAC_16,
		metadatapb.AudioFile_XHE_AAC_24:
		return true
	default:
		return false
	}
}

func extensionForFormat(format metadatapb.AudioFile_Format) string {
	switch format {
	case metadatapb.AudioFile_OGG_VORBIS_96, metadatapb.AudioFile_OGG_VORBIS_160, metadatapb.AudioFile_OGG_VORBIS_320:
		return "ogg"
	case metadatapb.AudioFile_MP3_96, metadatapb.AudioFile_MP3_160, metadatapb.AudioFile_MP3_160_ENC, metadatapb.AudioFile_MP3_256, metadatapb.AudioFile_MP3_320:
		return "mp3"
	case metadatapb.AudioFile_AAC_24, metadatapb.AudioFile_AAC_48, metadatapb.AudioFile_XHE_AAC_12, metadatapb.AudioFile_XHE_AAC_16, metadatapb.AudioFile_XHE_AAC_24:
		return "aac"
	default:
		return "bin"
	}
}

func contentTypeForFormat(format metadatapb.AudioFile_Format) string {
	switch format {
	case metadatapb.AudioFile_OGG_VORBIS_96, metadatapb.AudioFile_OGG_VORBIS_160, metadatapb.AudioFile_OGG_VORBIS_320:
		return "audio/ogg"
	case metadatapb.AudioFile_MP3_96, metadatapb.AudioFile_MP3_160, metadatapb.AudioFile_MP3_160_ENC, metadatapb.AudioFile_MP3_256, metadatapb.AudioFile_MP3_320:
		return "audio/mpeg"
	case metadatapb.AudioFile_AAC_24, metadatapb.AudioFile_AAC_48, metadatapb.AudioFile_XHE_AAC_12, metadatapb.AudioFile_XHE_AAC_16, metadatapb.AudioFile_XHE_AAC_24:
		return "audio/aac"
	default:
		return "application/octet-stream"
	}
}

func bitrateForFormat(format metadatapb.AudioFile_Format) int {
	switch format {
	case metadatapb.AudioFile_OGG_VORBIS_96, metadatapb.AudioFile_MP3_96:
		return 96
	case metadatapb.AudioFile_OGG_VORBIS_160, metadatapb.AudioFile_MP3_160, metadatapb.AudioFile_MP3_160_ENC:
		return 160
	case metadatapb.AudioFile_OGG_VORBIS_320, metadatapb.AudioFile_MP3_320:
		return 320
	case metadatapb.AudioFile_MP3_256:
		return 256
	case metadatapb.AudioFile_AAC_24, metadatapb.AudioFile_XHE_AAC_24:
		return 24
	case metadatapb.AudioFile_AAC_48:
		return 48
	case metadatapb.AudioFile_XHE_AAC_12:
		return 12
	case metadatapb.AudioFile_XHE_AAC_16:
		return 16
	default:
		return 0
	}
}

func formatRank(format metadatapb.AudioFile_Format) int {
	switch format {
	case metadatapb.AudioFile_MP3_96, metadatapb.AudioFile_MP3_160, metadatapb.AudioFile_MP3_160_ENC, metadatapb.AudioFile_MP3_256, metadatapb.AudioFile_MP3_320:
		return 0
	case metadatapb.AudioFile_OGG_VORBIS_96, metadatapb.AudioFile_OGG_VORBIS_160, metadatapb.AudioFile_OGG_VORBIS_320:
		return 1
	case metadatapb.AudioFile_AAC_24, metadatapb.AudioFile_AAC_48, metadatapb.AudioFile_XHE_AAC_12, metadatapb.AudioFile_XHE_AAC_16, metadatapb.AudioFile_XHE_AAC_24:
		return 2
	default:
		return 100
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func withAudioKeyTroubleshooting(err error) error {
	if err == nil {
		return nil
	}

	message := err.Error()
	if strings.Contains(message, "failed retrieving aes key with code 1") {
		return fmt.Errorf("%w\n\nTroubleshooting: Spotify authenticated the session but denied the track decryption key. This usually means the account is not allowed to perform interactive track playback/downloads with librespot-compatible clients. In practice, Spotify Premium is typically required for full track downloads", err)
	}

	return err
}

func isMediaRestricted(media *librespot.Media, country string) bool {
	country = strings.TrimSpace(country)
	if len(country) != 2 {
		return false
	}

	contains := func(list string) bool {
		for i := 0; i+1 < len(list); i += 2 {
			if strings.EqualFold(list[i:i+2], country) {
				return true
			}
		}
		return false
	}

	for _, restriction := range media.Restriction() {
		switch countries := restriction.CountryRestriction.(type) {
		case *metadatapb.Restriction_CountriesAllowed:
			if len(countries.CountriesAllowed) == 0 {
				return true
			}
			return !contains(countries.CountriesAllowed)
		case *metadatapb.Restriction_CountriesForbidden:
			return contains(countries.CountriesForbidden)
		}
	}

	return false
}
