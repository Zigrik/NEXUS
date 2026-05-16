package tunnel

import (
	"sync"
	"time"

	"nexus/pkg/protocol"
)

type Stream struct {
	ID          string
	multiplexer *Multiplexer
	closeCh     chan struct{}
	mu          sync.RWMutex
}

func NewStream(id string, m *Multiplexer) *Stream {
	return &Stream{
		ID:          id,
		multiplexer: m,
		closeCh:     make(chan struct{}),
	}
}

func (s *Stream) WriteRequest(req *protocol.RequestPayload) error {
	return s.multiplexer.SendRequest(s.ID, req)
}

func (s *Stream) ReadResponse(requestID string) (*protocol.ResponsePayload, error) {
	ch := s.multiplexer.RegisterPendingRequest(requestID)

	select {
	case resp := <-ch:
		s.multiplexer.UnregisterPendingRequest(requestID)
		return resp, nil
	case <-time.After(30 * time.Second):
		s.multiplexer.UnregisterPendingRequest(requestID)
		return nil, ErrTimeout
	case <-s.closeCh:
		return nil, ErrStreamClosed
	}
}

func (s *Stream) Close() error {
	select {
	case <-s.closeCh:
		return nil
	default:
		close(s.closeCh)
		s.multiplexer.CloseStream(s.ID)
		return nil
	}
}

var (
	ErrTimeout      = &StreamError{"request timeout"}
	ErrStreamClosed = &StreamError{"stream closed"}
)

type StreamError struct {
	message string
}

func (e *StreamError) Error() string {
	return e.message
}
