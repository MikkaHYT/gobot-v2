package ripper

import (
	"sync"
)

type slotManager struct {
	mu     sync.Mutex
	active map[string]int
}

func newSlotManager() *slotManager {
	return &slotManager{
		active: make(map[string]int),
	}
}

func (s *slotManager) tryAcquire(userID string, maxSlots int) bool {
	if userID == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active[userID] >= maxSlots {
		return false
	}
	s.active[userID]++
	return true
}

func (s *slotManager) release(userID string) {
	if userID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.active[userID]
	if current <= 1 {
		delete(s.active, userID)
		return
	}
	s.active[userID]--
}
