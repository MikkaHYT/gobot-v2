package roles

import "sync"

type roleLockEntry struct {
	mu   sync.Mutex
	refs int
}

type roleLockSet struct {
	mu      sync.Mutex
	entries map[string]*roleLockEntry
}

var sharedRoleLocks = roleLockSet{entries: make(map[string]*roleLockEntry)}

func (s *roleLockSet) lock(guildID, userID, roleID string) func() {
	key := guildID + "\x00" + userID

	s.mu.Lock()
	entry := s.entries[key]
	if entry == nil {
		entry = &roleLockEntry{}
		s.entries[key] = entry
	}
	entry.refs++
	s.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()

		s.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(s.entries, key)
		}
		s.mu.Unlock()
	}
}
