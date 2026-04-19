package appapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// SessionState is the bridge-facing persisted playback/session snapshot.
type SessionState struct {
	ContextURI     string  `json:"contextURI"`
	ContextName    string  `json:"contextName"`
	TrackURI       string  `json:"trackURI"`
	TrackName      string  `json:"trackName"`
	TrackArtist    string  `json:"trackArtist"`
	AlbumArtURL    string  `json:"albumArtURL"`
	PositionMs     int     `json:"positionMs"`
	UserVolume     float64 `json:"userVolume"`
	DataSavingMode bool    `json:"dataSavingMode"`
	SchemaVersion  int     `json:"schemaVersion"`
}

// SessionManager owns persisted session state. It is additive and can run
// alongside existing QML Settings until full cutover.
type SessionManager struct {
	mu       sync.RWMutex
	state    SessionState
	filePath string
}

func NewSessionManager() *SessionManager {
	return &SessionManager{state: SessionState{SchemaVersion: 1, UserVolume: 0.8}}
}

func (m *SessionManager) State() SessionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *SessionManager) SetState(next SessionState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next.SchemaVersion = 1
	if next.PositionMs < 0 {
		next.PositionMs = 0
	}
	m.state = sanitizeState(next)
}

func (m *SessionManager) Update(fn func(*SessionState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.state)
	if m.state.PositionMs < 0 {
		m.state.PositionMs = 0
	}
	m.state.SchemaVersion = 1
	m.state = sanitizeState(m.state)
}

func (m *SessionManager) SetFilePath(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filePath = strings.TrimSpace(path)
}

func (m *SessionManager) FilePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.filePath
}

func (m *SessionManager) ResolveDefaultFilePath() (string, error) {
	base, err := resolveWritableBaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "voxora")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}
	return filepath.Join(dir, "playback_session.json"), nil
}

func (m *SessionManager) EnsureDefaultPath() error {
	if strings.TrimSpace(m.FilePath()) != "" {
		return nil
	}
	path, err := m.ResolveDefaultFilePath()
	if err != nil {
		return err
	}
	m.SetFilePath(path)
	return nil
}

func (m *SessionManager) Load() (SessionState, error) {
	if err := m.EnsureDefaultPath(); err != nil {
		return SessionState{}, err
	}
	path := m.FilePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m.State(), nil
		}
		return SessionState{}, fmt.Errorf("read session file: %w", err)
	}
	if len(raw) == 0 {
		return m.State(), nil
	}
	var s SessionState
	if err := json.Unmarshal(raw, &s); err != nil {
		return SessionState{}, fmt.Errorf("parse session file: %w", err)
	}
	s = sanitizeState(s)
	s.SchemaVersion = 1
	m.SetState(s)
	return s, nil
}

func (m *SessionManager) Save() error {
	if err := m.EnsureDefaultPath(); err != nil {
		return err
	}
	path := m.FilePath()
	state := m.State()
	state.SchemaVersion = 1
	state = sanitizeState(state)

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session state: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return fmt.Errorf("write temp session file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("commit session file: %w", err)
	}
	return nil
}

func (m *SessionManager) Clear() error {
	if err := m.EnsureDefaultPath(); err != nil {
		return err
	}
	m.SetState(SessionState{SchemaVersion: 1})
	path := m.FilePath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear session file: %w", err)
	}
	return nil
}

func sanitizeState(s SessionState) SessionState {
	s.ContextURI = strings.TrimSpace(s.ContextURI)
	s.ContextName = strings.TrimSpace(s.ContextName)
	s.TrackURI = strings.TrimSpace(s.TrackURI)
	s.TrackName = strings.TrimSpace(s.TrackName)
	s.TrackArtist = strings.TrimSpace(s.TrackArtist)
	s.AlbumArtURL = strings.TrimSpace(s.AlbumArtURL)
	if s.PositionMs < 0 {
		s.PositionMs = 0
	}
	if s.UserVolume < 0 {
		s.UserVolume = 0
	}
	if s.UserVolume > 1 {
		s.UserVolume = 1
	}
	if s.UserVolume == 0 {
		// Preserve historical behavior for older session files that didn't have this field.
		s.UserVolume = 0.8
	}
	if s.SchemaVersion <= 0 {
		s.SchemaVersion = 1
	}
	return s
}

func resolveWritableBaseDir() (string, error) {
	if env := strings.TrimSpace(os.Getenv("VOXORA_STATE_DIR")); env != "" {
		if err := os.MkdirAll(env, 0o755); err != nil {
			return "", fmt.Errorf("create VOXORA_STATE_DIR: %w", err)
		}
		return env, nil
	}

	if runtime.GOOS == "android" {
		// On Android, prefer app-scoped external files dir if provided by launcher,
		// then fallback to per-app config dir.
		for _, key := range []string{"ANDROID_APP_FILES_DIR", "EXTERNAL_STORAGE", "HOME"} {
			if val := strings.TrimSpace(os.Getenv(key)); val != "" {
				if err := os.MkdirAll(val, 0o755); err == nil {
					return val, nil
				}
			}
		}
	}

	if cfg, err := os.UserConfigDir(); err == nil && strings.TrimSpace(cfg) != "" {
		if err := os.MkdirAll(cfg, 0o755); err == nil {
			return cfg, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		fallback := filepath.Join(home, ".config")
		if err := os.MkdirAll(fallback, 0o755); err == nil {
			return fallback, nil
		}
	}
	return "", fmt.Errorf("unable to resolve writable base dir")
}
