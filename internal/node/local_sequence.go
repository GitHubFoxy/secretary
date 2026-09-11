package node

import "errors"

var ErrUnsentNodeEvents = errors.New("node: unsent durable events must be flushed first")

// ReserveControlSequence reserves a durable sequence for heartbeat/inventory
// only when every lower durable outbox event has already been written on this
// connection. This prevents a concurrently queued Attempt event from arriving
// after a higher heartbeat sequence and being rejected as a replay.
func (s *LocalStore) ReserveControlSequence(sentThrough uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pending := range s.state.Outbox {
		if pending.Sequence > sentThrough {
			return 0, ErrUnsentNodeEvents
		}
	}
	sequence := s.state.NextSequence
	if sequence == 0 {
		sequence = 1
	}
	s.state.NextSequence = sequence + 1
	if err := s.persistLocked(); err != nil {
		s.state.NextSequence = sequence
		return 0, err
	}
	return sequence, nil
}
