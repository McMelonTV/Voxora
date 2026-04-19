package appapi

import "sync"

// NavigationEntry mirrors lightweight QML navigation history context.
type NavigationEntry struct {
	FromViewMode string `json:"fromViewMode"`
}

// NavigationState is bridge-facing navigation data.
type NavigationState struct {
	Stack             []NavigationEntry `json:"stack"`
	ActiveContextURI  string            `json:"activeContextURI"`
	ActiveContextName string            `json:"activeContextName"`
}

// NavigationStackManager owns non-visual navigation state.
type NavigationStackManager struct {
	mu    sync.RWMutex
	state NavigationState
}

func NewNavigationStackManager() *NavigationStackManager {
	return &NavigationStackManager{}
}

func (m *NavigationStackManager) State() NavigationState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copyState := m.state
	copyState.Stack = append([]NavigationEntry(nil), m.state.Stack...)
	return copyState
}

func (m *NavigationStackManager) OpenCollection(uri, name, fromViewMode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.ActiveContextURI = uri
	m.state.ActiveContextName = name
	m.state.Stack = append(m.state.Stack, NavigationEntry{FromViewMode: fromViewMode})
}

func (m *NavigationStackManager) CanGoBack() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.state.Stack) > 0
}

func (m *NavigationStackManager) Back() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.state.Stack) == 0 {
		return false
	}
	m.state.Stack = m.state.Stack[:len(m.state.Stack)-1]
	return true
}

func (m *NavigationStackManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Stack = nil
	m.state.ActiveContextURI = ""
	m.state.ActiveContextName = ""
}

func (m *NavigationStackManager) Depth() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.state.Stack)
}
