package assistant

import (
	"sync"

	"github.com/google/uuid"
)

// Conversation is the per-session state the chat handler needs to remember
// across HTTP requests so "yes" / "scan the second one" actually refers to a
// previous proposal.
//
// State is intentionally tiny: just the most recent candidate list. Long-term
// chat history is rendered client-side; the server doesn't need it to make
// any current-turn decision.
type Conversation struct {
	ID                string
	PendingCandidates []Candidate
	LastQueriedTarget string
}

// ConversationStore is an in-memory map of conversation_id → state.
// Concurrent access from many HTTP handlers is expected, hence the RWMutex.
//
// v0.5 keeps everything in memory. If the user wants resume-after-restart
// semantics later, swap this for a disk-backed store under
// ~/.assay/conversations/<id>/state.json without touching callers.
type ConversationStore struct {
	mu   sync.RWMutex
	conv map[string]*Conversation
}

// NewConversationStore returns a ready-to-use empty store.
func NewConversationStore() *ConversationStore {
	return &ConversationStore{conv: make(map[string]*Conversation)}
}

// GetOrCreate returns the conversation for `id`. When `id` is empty or
// unknown, a fresh conversation is allocated and returned.
func (s *ConversationStore) GetOrCreate(id string) *Conversation {
	if id != "" {
		s.mu.RLock()
		c, ok := s.conv[id]
		s.mu.RUnlock()
		if ok {
			return c
		}
	}
	c := &Conversation{ID: uuid.NewString()}
	s.mu.Lock()
	s.conv[c.ID] = c
	s.mu.Unlock()
	return c
}

// Set replaces the state for `id`. Used after a turn produces a new proposal.
func (s *ConversationStore) Set(c *Conversation) {
	if c == nil || c.ID == "" {
		return
	}
	s.mu.Lock()
	s.conv[c.ID] = c
	s.mu.Unlock()
}
