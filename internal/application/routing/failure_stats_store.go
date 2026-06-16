package routing

import (
	"sync/atomic"

	domainrouting "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
)

// FailureStatsStore provides lock-free read access to the latest FailureStats
// snapshot via an atomic pointer swap.
type FailureStatsStore struct {
	current atomic.Pointer[domainrouting.FailureStats]
}

// NewFailureStatsStore creates a new store. Get() returns nil before the first Update.
func NewFailureStatsStore() *FailureStatsStore {
	return &FailureStatsStore{}
}

// Get returns the current FailureStats snapshot, or nil if no refresh has occurred yet.
func (s *FailureStatsStore) Get() *domainrouting.FailureStats {
	return s.current.Load()
}

// Update atomically replaces the current snapshot with a new one.
func (s *FailureStatsStore) Update(stats *domainrouting.FailureStats) {
	s.current.Store(stats)
}
