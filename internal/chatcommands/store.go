package chatcommands

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrQueueItemNotFound = errors.New("queue item not found")

type Store interface {
	Controls(context.Context, string) (SessionControls, error)
	SaveControls(context.Context, SessionControls) error
	Enqueue(context.Context, QueueItem) error
	Queue(context.Context, string) ([]QueueItem, error)
	ReplaceQueue(context.Context, string, []QueueItem) error
}

// MemoryStore is deterministic and primarily intended for package tests and
// embedding prototypes. Production integration should back Store with the
// durable chat tables introduced by migration 0010.
type MemoryStore struct {
	mu       sync.Mutex
	controls map[string]SessionControls
	queue    map[string][]QueueItem
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{controls: map[string]SessionControls{}, queue: map[string][]QueueItem{}}
}
func (m *MemoryStore) Controls(_ context.Context, id string) (SessionControls, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.controls[id]
	if !ok {
		c = SessionControls{SessionID: id, BusyMode: BusyQueue, ApprovalMode: ApprovalManual, ApprovalLevel: ApprovalMedium, UpdatedAt: time.Now().UTC()}
	}
	return c, nil
}
func (m *MemoryStore) SaveControls(_ context.Context, c SessionControls) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.UpdatedAt = time.Now().UTC()
	m.controls[c.SessionID] = c
	return nil
}
func (m *MemoryStore) Enqueue(_ context.Context, q QueueItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.queue[q.SessionID]
	q.Position = int64(len(items) + 1)
	if q.CreatedAt.IsZero() {
		q.CreatedAt = time.Now().UTC()
	}
	q.UpdatedAt = q.CreatedAt
	if q.Status == "" {
		q.Status = QueuePending
	}
	m.queue[q.SessionID] = append(items, q)
	return nil
}
func (m *MemoryStore) Queue(_ context.Context, id string) ([]QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]QueueItem(nil), m.queue[id]...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}
func (m *MemoryStore) ReplaceQueue(_ context.Context, id string, items []QueueItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for i := range items {
		items[i].Position = int64(i + 1)
		items[i].UpdatedAt = now
	}
	m.queue[id] = append([]QueueItem(nil), items...)
	return nil
}
