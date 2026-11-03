package main

import (
	"sync"
	"time"
)

// History 是单个 WebSocket 连接内的多轮对话记忆。
// 按连接隔离，避免多设备/多会话串话。
type History struct {
	mu     sync.Mutex
	cfg    MemoryConfig
	turns  []ChatTurn
	lastAt time.Time
}

func NewHistory(cfg MemoryConfig) *History {
	return &History{cfg: cfg.normalized()}
}

// Get 返回窗口内的历史（最多 MaxTurns 轮）。
// 超过时间窗口会自动清空，避免上下文污染。
func (h *History) Get() []ChatTurn {
	if !h.cfg.Enabled {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.lastAt.IsZero() {
		return nil
	}
	if time.Since(h.lastAt) > time.Duration(h.cfg.WindowSeconds)*time.Second {
		h.turns = nil
		return nil
	}
	return append([]ChatTurn(nil), h.turns...)
}

// Append 记录一轮问答。
func (h *History) Append(q, a string) {
	if !h.cfg.Enabled || a == "" || q == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	h.turns = append(h.turns, ChatTurn{Q: q, A: a})
	if len(h.turns) > h.cfg.MaxTurns {
		h.turns = h.turns[len(h.turns)-h.cfg.MaxTurns:]
	}
	h.lastAt = time.Now()
}

// Reset 清空记忆。
func (h *History) Reset() {
	h.mu.Lock()
	h.turns = nil
	h.lastAt = time.Time{}
	h.mu.Unlock()
}
