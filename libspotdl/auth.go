package libspotdl

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	devicespb "github.com/devgianlu/go-librespot/proto/spotify/connectstate/devices"
	librespotsession "github.com/devgianlu/go-librespot/session"
	"golang.org/x/oauth2"
	spotifyoauth2 "golang.org/x/oauth2/spotify"
)

const defaultCallbackPort = 36842

type credentialCache struct {
	DeviceID          string `json:"device_id"`
	Username          string `json:"username"`
	StoredCredentials string `json:"stored_credentials"`
	OAuthAccessToken  string `json:"oauth_access_token,omitempty"`
}

type spotifyTokenAuth struct {
	Username string
	Token    string
}

// InteractiveAuthStart contains the browser URL and flow handle for
// application-driven OAuth login.
type InteractiveAuthStart struct {
	AuthURL      string
	CallbackPort int
	Flow         *InteractiveAuthFlow
}

// InteractiveAuthResult is the final result for an OAuth flow.
type InteractiveAuthResult struct {
	Username string
	Token    string
}

// InteractiveAuthFlow represents one pending OAuth browser flow.
type InteractiveAuthFlow struct {
	cancel context.CancelFunc
	result chan interactiveAuthOutcome
	once   sync.Once
}

type interactiveAuthOutcome struct {
	result InteractiveAuthResult
	err    error
}

var ErrNoStoredCredentials = errors.New("no stored spotify credentials found")
var ErrNoCachedOAuthAccessToken = errors.New("no cached spotify oauth access token found")

func spotifyOAuthClientID() string {
	if configured := strings.TrimSpace(os.Getenv("SPOTIFY_WEB_API_CLIENT_ID")); configured != "" {
		return configured
	}
	if configured := strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_ID")); configured != "" {
		return configured
	}
	return librespot.ClientIdHex
}

func usingDefaultSpotifyOAuthClientID() bool {
	return spotifyOAuthClientID() == librespot.ClientIdHex
}

func defaultCredentialsFile() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}

	return filepath.Join(configDir, "libspotdl", "credentials.json"), nil
}

func resolveCredentialsFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return defaultCredentialsFile()
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	return filepath.Clean(path), nil
}

// ResolveCredentialsFile returns the effective credentials cache path that
// libspotdl will use for stored credentials and device ID caching.
func ResolveCredentialsFile(path string) (string, error) {
	return resolveCredentialsFile(path)
}

func randomDeviceID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate device id: %w", err)
	}

	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4],
		buf[4:6],
		buf[6:8],
		buf[8:10],
		buf[10:16],
	), nil
}

func isLegacyHexDeviceID(deviceID string) bool {
	deviceID = strings.TrimSpace(deviceID)
	if len(deviceID) != 40 {
		return false
	}
	_, err := hex.DecodeString(deviceID)
	return err == nil
}

func loadCredentialCache(path string) (*credentialCache, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials cache: %w", err)
	}

	var cache credentialCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("decode credentials cache: %w", err)
	}

	return &cache, nil
}

func saveCredentialCache(path, deviceID string, sess *librespotsession.Session) error {
	existing, err := loadCredentialCache(path)
	if err != nil {
		return err
	}

	oauthAccessToken := ""
	if existing != nil {
		oauthAccessToken = strings.TrimSpace(existing.OAuthAccessToken)
	}

	cache := credentialCache{
		DeviceID:          deviceID,
		Username:          sess.Username(),
		StoredCredentials: base64.StdEncoding.EncodeToString(sess.StoredCredentials()),
		OAuthAccessToken:  oauthAccessToken,
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials cache: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create credentials cache dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials cache: %w", err)
	}

	return nil
}

func saveOAuthAccessToken(path, accessToken string) error {
	cache, err := loadCredentialCache(path)
	if err != nil {
		return err
	}
	if cache == nil {
		cache = &credentialCache{}
	}

	cache.OAuthAccessToken = strings.TrimSpace(accessToken)

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials cache: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create credentials cache dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials cache: %w", err)
	}

	return nil
}

func withSessionTroubleshooting(err error) error {
	if err == nil {
		return nil
	}

	message := err.Error()
	if strings.Contains(message, "apresolve.spotify.com") && strings.Contains(message, "0.0.0.0:443") {
		return fmt.Errorf("%w\n\nTroubleshooting: apresolve.spotify.com is resolving to 0.0.0.0. Check /etc/hosts, DNS sinkhole/adblock rules, VPN filters, or firewall tooling blocking Spotify resolver hosts. libspotdl cannot authenticate until apresolve.spotify.com resolves to a real Spotify address", err)
	}

	if strings.Contains(message, "apresolve.spotify.com") && strings.Contains(strings.ToLower(message), "proxy") {
		return fmt.Errorf("%w\n\nTroubleshooting: check HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, and NO_PROXY. A broken proxy setting can prevent libspotdl from reaching Spotify's resolver", err)
	}

	return err
}

func acquireInteractiveSpotifyToken(ctx context.Context, log librespot.Logger, callbackPort int) (*spotifyTokenAuth, error) {
	start, err := StartInteractiveAuthFlow(ctx, log, callbackPort)
	if err != nil {
		return nil, err
	}

	log.Infof("to complete authentication visit the following link: %s", start.AuthURL)

	result, err := start.Flow.Wait(ctx)
	if err != nil {
		return nil, err
	}

	return &spotifyTokenAuth{
		Username: result.Username,
		Token:    result.Token,
	}, nil
}

// StartInteractiveAuthFlow starts an OAuth callback server and returns a URL the
// caller should open in a browser.
func StartInteractiveAuthFlow(parent context.Context, log librespot.Logger, callbackPort int) (*InteractiveAuthStart, error) {
	serverCtx, serverCancel := context.WithCancel(context.Background())

	callbackPort, codeCh, err := librespotsession.NewOAuth2Server(serverCtx, log, callbackPort)
	if err != nil {
		serverCancel()
		return nil, fmt.Errorf("failed initializing oauth2 server: %w", err)
	}

	oauthConf := &oauth2.Config{
		ClientID:    spotifyOAuthClientID(),
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/login", callbackPort),
		Scopes: []string{
			"streaming",
			"playlist-read-private",
			"playlist-read-collaborative",
			"user-library-read",
		},
		Endpoint: spotifyoauth2.Endpoint,
	}

	verifier := oauth2.GenerateVerifier()
	authURL := oauthConf.AuthCodeURL("", oauth2.S256ChallengeOption(verifier))

	flow := &InteractiveAuthFlow{
		cancel: serverCancel,
		result: make(chan interactiveAuthOutcome, 1),
	}

	go func() {
		defer serverCancel()

		var outcome interactiveAuthOutcome

		var code string
		select {
		case <-parent.Done():
			outcome.err = parent.Err()
		case code = <-codeCh:
			if strings.TrimSpace(code) == "" {
				outcome.err = errors.New("spotify oauth callback returned no authorization code")
				break
			}

			token, err := oauthConf.Exchange(parent, code, oauth2.VerifierOption(verifier))
			if err != nil {
				outcome.err = fmt.Errorf("failed exchanging oauth2 code: %w", err)
				break
			}

			username, _ := token.Extra("username").(string)
			accessToken := strings.TrimSpace(token.AccessToken)
			if accessToken == "" {
				outcome.err = errors.New("spotify oauth token response did not include an access token")
				break
			}

			outcome.result = InteractiveAuthResult{
				Username: strings.TrimSpace(username),
				Token:    accessToken,
			}
		}

		flow.result <- outcome
		close(flow.result)
	}()

	return &InteractiveAuthStart{
		AuthURL:      authURL,
		CallbackPort: callbackPort,
		Flow:         flow,
	}, nil
}

// Wait blocks until the flow succeeds, fails, or ctx is canceled.
func (f *InteractiveAuthFlow) Wait(ctx context.Context) (InteractiveAuthResult, error) {
	if f == nil {
		return InteractiveAuthResult{}, errors.New("nil interactive auth flow")
	}

	select {
	case <-ctx.Done():
		f.Cancel()
		return InteractiveAuthResult{}, ctx.Err()
	case outcome, ok := <-f.result:
		if !ok {
			return InteractiveAuthResult{}, errors.New("interactive auth flow ended unexpectedly")
		}
		return outcome.result, outcome.err
	}
}

// Cancel cancels the callback server and pending waiters.
func (f *InteractiveAuthFlow) Cancel() {
	if f == nil || f.cancel == nil {
		return
	}
	f.once.Do(f.cancel)
}

// PersistInteractiveAuthResult exchanges an OAuth token for stored credentials
// and writes them to the configured credentials cache.
func PersistInteractiveAuthResult(ctx context.Context, cfg Config, result InteractiveAuthResult) error {
	cfg.Auth.IgnoreStoredCredentials = true
	cfg.Auth.AccessToken = strings.TrimSpace(result.Token)
	if cfg.Auth.AccessToken == "" {
		return errors.New("empty spotify access token")
	}
	cfg.Auth.Username = strings.TrimSpace(result.Username)

	log := cfg.Logger
	if log == nil {
		log = newDefaultLogger()
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	sess, err := newAuthenticatedSession(ctx, cfg, log, client)
	if err != nil {
		return err
	}
	sess.Close()

	credentialsFile, err := resolveCredentialsFile(cfg.Auth.CredentialsFile)
	if err != nil {
		return err
	}

	if err := saveOAuthAccessToken(credentialsFile, result.Token); err != nil {
		return err
	}

	return nil
}

// CachedOAuthAccessToken returns the cached OAuth access token from the
// credentials cache file.
func CachedOAuthAccessToken(credentialsFile string) (string, error) {
	resolved, err := resolveCredentialsFile(credentialsFile)
	if err != nil {
		return "", err
	}

	cache, err := loadCredentialCache(resolved)
	if err != nil {
		return "", err
	}
	if cache == nil {
		return "", ErrNoCachedOAuthAccessToken
	}

	token := strings.TrimSpace(cache.OAuthAccessToken)
	if token == "" {
		return "", ErrNoCachedOAuthAccessToken
	}

	return token, nil
}

// TryStoredCredentialsSession attempts to authenticate using only cached
// stored credentials. It does not trigger the browser OAuth flow.
func TryStoredCredentialsSession(ctx context.Context, cfg Config) error {
	credentialsFile, err := resolveCredentialsFile(cfg.Auth.CredentialsFile)
	if err != nil {
		return err
	}

	cache, err := loadCredentialCache(credentialsFile)
	if err != nil {
		return err
	}
	if cache == nil || strings.TrimSpace(cache.Username) == "" || strings.TrimSpace(cache.StoredCredentials) == "" {
		return ErrNoStoredCredentials
	}

	stored, err := base64.StdEncoding.DecodeString(cache.StoredCredentials)
	if err != nil {
		return fmt.Errorf("decode cached stored credentials: %w", err)
	}

	deviceID := strings.TrimSpace(cfg.Auth.DeviceID)
	if deviceID == "" && !isLegacyHexDeviceID(cache.DeviceID) {
		deviceID = strings.TrimSpace(cache.DeviceID)
	}
	if deviceID == "" {
		deviceID, err = randomDeviceID()
		if err != nil {
			return err
		}
	}

	log := cfg.Logger
	if log == nil {
		log = newDefaultLogger()
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	sess, err := librespotsession.NewSessionFromOptions(ctx, &librespotsession.Options{
		Log:        log,
		Client:     client,
		DeviceType: devicespb.DeviceType_COMPUTER,
		DeviceId:   deviceID,
		Credentials: librespotsession.StoredCredentials{
			Username: cache.Username,
			Data:     stored,
		},
	})
	if err != nil {
		return withSessionTroubleshooting(fmt.Errorf("authenticate with stored credentials: %w", err))
	}
	sess.Close()
	return nil
}

// HasStoredCredentials returns whether a usable stored-credentials cache is
// present. It does not perform network calls or authenticate.
func HasStoredCredentials(credentialsFile string) (bool, error) {
	resolved, err := resolveCredentialsFile(credentialsFile)
	if err != nil {
		return false, err
	}

	cache, err := loadCredentialCache(resolved)
	if err != nil {
		return false, err
	}
	if cache == nil {
		return false, nil
	}

	return strings.TrimSpace(cache.Username) != "" && strings.TrimSpace(cache.StoredCredentials) != "", nil
}

// IsSpotifyCredentialRefusedError returns true when an auth failure appears to
// come from rejected credentials rather than network/connectivity issues.
func IsSpotifyCredentialRefusedError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	networkHints := []string{
		"apresolve.spotify.com",
		"dial tcp",
		"connection refused",
		"no such host",
		"i/o timeout",
		"context deadline exceeded",
		"network is unreachable",
		"proxy",
		"tls handshake timeout",
		"temporary failure in name resolution",
	}
	for _, hint := range networkHints {
		if strings.Contains(msg, hint) {
			return false
		}
	}

	authHints := []string{
		"bad credentials",
		"invalid credentials",
		"authentication failed",
		"auth failed",
		"login failed",
		"login5",
		"could not authenticate",
		"couldn't authenticate",
		"forbidden",
		"status 401",
		"status 403",
	}
	for _, hint := range authHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}

	return false
}

func resolveSessionCredentials(ctx context.Context, cfg Config, log librespot.Logger, client *http.Client, deviceID string, cache *credentialCache) (any, error) {
	switch {
	case strings.TrimSpace(cfg.Auth.AccessToken) != "":
		return librespotsession.SpotifyTokenCredentials{
			Username: strings.TrimSpace(cfg.Auth.Username),
			Token:    strings.TrimSpace(cfg.Auth.AccessToken),
		}, nil
	case !cfg.Auth.IgnoreStoredCredentials && cache != nil && cache.Username != "" && cache.StoredCredentials != "":
		stored, err := base64.StdEncoding.DecodeString(cache.StoredCredentials)
		if err != nil {
			return nil, fmt.Errorf("decode cached stored credentials: %w", err)
		}
		return librespotsession.StoredCredentials{
			Username: cache.Username,
			Data:     stored,
		}, nil
	default:
		callbackPort := cfg.Auth.CallbackPort
		if callbackPort == 0 {
			callbackPort = defaultCallbackPort
		}

		auth, err := acquireInteractiveSpotifyToken(ctx, log, callbackPort)
		if err != nil {
			return nil, err
		}

		return librespotsession.SpotifyTokenCredentials{
			// Match Rust librespot behavior: Spotify token auth does not require
			// sending username in AP login credentials.
			Username: "",
			Token:    auth.Token,
		}, nil
	}
}

func newAuthenticatedSession(ctx context.Context, cfg Config, log librespot.Logger, client *http.Client) (*librespotsession.Session, error) {
	credentialsFile, err := resolveCredentialsFile(cfg.Auth.CredentialsFile)
	if err != nil {
		return nil, err
	}

	cache, err := loadCredentialCache(credentialsFile)
	if err != nil {
		return nil, err
	}

	deviceID := strings.TrimSpace(cfg.Auth.DeviceID)
	if deviceID == "" && cache != nil && !isLegacyHexDeviceID(cache.DeviceID) {
		deviceID = strings.TrimSpace(cache.DeviceID)
	}
	if deviceID == "" {
		deviceID, err = randomDeviceID()
		if err != nil {
			return nil, err
		}
	}

	creds, err := resolveSessionCredentials(ctx, cfg, log, client, deviceID, cache)
	if err != nil {
		return nil, withSessionTroubleshooting(err)
	}

	sess, err := librespotsession.NewSessionFromOptions(ctx, &librespotsession.Options{
		Log:         log,
		Client:      client,
		DeviceType:  devicespb.DeviceType_COMPUTER,
		DeviceId:    deviceID,
		Credentials: creds,
	})
	if err != nil {
		return nil, fmt.Errorf("create spotify session: %w", withSessionTroubleshooting(err))
	}

	if err := saveCredentialCache(credentialsFile, deviceID, sess); err != nil {
		sess.Close()
		return nil, err
	}

	return sess, nil
}
