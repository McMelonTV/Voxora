package appapi

import "math"

// SeekDecisionInput contains all data needed to evaluate a seek request.
type SeekDecisionInput struct {
	Ratio                float64
	EffectiveDurationMs  int
	CurrentPlayingPath   string
	IsPartStreamSource   bool
	SeekEnabledForSource bool
	StreamCacheReady     bool
	StreamCachePath      string
	StreamBufferedBytes  int64
	StreamBufferedTotal  int64
}

// SeekDecision is returned to QML, which executes MediaPlayer actions.
type SeekDecision struct {
	Allow               bool    `json:"allow"`
	Reason              string  `json:"reason"`
	ShouldSwitchToCache bool    `json:"shouldSwitchToCache"`
	TargetPath          string  `json:"targetPath"`
	TargetPosMs         int     `json:"targetPosMs"`
	TargetRatio         float64 `json:"targetRatio"`
}

// StreamManager owns non-visual seek policy decisions.
type StreamManager struct{}

func NewStreamManager() *StreamManager {
	return &StreamManager{}
}

// RecoveryDecisionInput captures playback-stop context for recovery policy.
type RecoveryDecisionInput struct {
	ManualStopRequested   bool
	StreamRecovering      bool
	HoldStoppedTrackState bool
	StreamSourceSwitching bool
	CurrentPlayingPath    string
	CachePath             string
	StreamPlayPath        string
	LikelyNaturalEnd      bool
	HasNewBufferedData    bool
	StillDownloading      bool
	StreamRecoverAttempts int
}

// RecoveryDecision tells QML whether to execute recovery actions.
type RecoveryDecision struct {
	ShouldRecover bool   `json:"shouldRecover"`
	Reason        string `json:"reason"`
}

func (m *StreamManager) EvaluateSeek(in SeekDecisionInput) SeekDecision {
	if in.EffectiveDurationMs <= 0 {
		return SeekDecision{Allow: false, Reason: "Duration unavailable"}
	}

	ratio := in.Ratio
	if math.IsNaN(ratio) {
		ratio = 0
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	targetPos := int(float64(in.EffectiveDurationMs) * ratio)
	isLiveFifo := len(in.CurrentPlayingPath) >= 10 && in.CurrentPlayingPath[len(in.CurrentPlayingPath)-10:] == ".live.fifo"
	isPartOrLive := isLiveFifo || in.IsPartStreamSource

	if !in.SeekEnabledForSource {
		if isPartOrLive && in.StreamCacheReady && in.StreamCachePath != "" {
			return SeekDecision{
				Allow:               true,
				ShouldSwitchToCache: true,
				TargetPath:          in.StreamCachePath,
				TargetPosMs:         targetPos,
				TargetRatio:         ratio,
			}
		}
		return SeekDecision{Allow: false, Reason: "Seek is available after stream buffering finishes"}
	}

	if isPartOrLive {
		canSeekInCache := in.StreamCacheReady && in.StreamCachePath != ""
		if canSeekInCache && in.StreamBufferedTotal > 0 && in.StreamBufferedBytes >= 0 {
			bufferedRatio := float64(in.StreamBufferedBytes) / float64(in.StreamBufferedTotal)
			if bufferedRatio < 0 {
				bufferedRatio = 0
			}
			if bufferedRatio > 1 {
				bufferedRatio = 1
			}
			canSeekInCache = ratio <= math.Max(0, bufferedRatio-0.01)
		}
		if canSeekInCache {
			return SeekDecision{
				Allow:               true,
				ShouldSwitchToCache: true,
				TargetPath:          in.StreamCachePath,
				TargetPosMs:         targetPos,
				TargetRatio:         ratio,
			}
		}
		return SeekDecision{Allow: false, Reason: "Seek target not buffered yet"}
	}

	return SeekDecision{Allow: true, TargetPosMs: targetPos, TargetRatio: ratio}
}

func (m *StreamManager) EvaluateRecovery(in RecoveryDecisionInput) RecoveryDecision {
	if in.ManualStopRequested {
		return RecoveryDecision{ShouldRecover: false, Reason: "manual stop requested"}
	}
	if in.StreamRecovering {
		return RecoveryDecision{ShouldRecover: false, Reason: "recovery already in progress"}
	}
	if in.HoldStoppedTrackState {
		return RecoveryDecision{ShouldRecover: false, Reason: "hold stopped track state"}
	}
	if in.StreamSourceSwitching {
		return RecoveryDecision{ShouldRecover: false, Reason: "source switching in progress"}
	}
	if in.StreamRecoverAttempts >= 6 {
		return RecoveryDecision{ShouldRecover: false, Reason: "recovery attempts exhausted"}
	}

	isPotentialStreamStop := in.CurrentPlayingPath != "" && (endsWith(in.CurrentPlayingPath, ".live.fifo") ||
		(in.CachePath != "" && in.CurrentPlayingPath == in.CachePath) ||
		(in.StreamPlayPath != "" && in.StreamPlayPath == in.CurrentPlayingPath))
	if !isPotentialStreamStop {
		return RecoveryDecision{ShouldRecover: false, Reason: "not a stream stop"}
	}

	isCacheStop := in.CachePath != "" && in.CurrentPlayingPath == in.CachePath
	recoverableStop := !in.LikelyNaturalEnd && (endsWith(in.CurrentPlayingPath, ".live.fifo") ||
		in.HasNewBufferedData ||
		in.StillDownloading ||
		isCacheStop)
	if !recoverableStop {
		return RecoveryDecision{ShouldRecover: false, Reason: "stop not recoverable"}
	}

	return RecoveryDecision{ShouldRecover: true, Reason: "recoverable stream stop"}
}

func endsWith(s, suffix string) bool {
	if len(suffix) == 0 {
		return true
	}
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
