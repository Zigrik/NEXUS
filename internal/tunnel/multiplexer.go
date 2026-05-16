package tunnel

import (
	"encoding/json"
	"net"
	"sync"

	"nexus/pkg/logger"
	"nexus/pkg/protocol"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type StreamHandler interface {
	OnStreamClosed(streamID string)
}

type Multiplexer struct {
	conn            net.Conn
	handler         StreamHandler
	streams         map[string]*Stream
	streamMu        sync.RWMutex
	pendingRequests map[string]chan *protocol.ResponsePayload
	pendingMu       sync.RWMutex
	closeCh         chan struct{}
	wg              sync.WaitGroup
}

func NewMultiplexer(conn net.Conn, handler StreamHandler) *Multiplexer {
	return &Multiplexer{
		conn:            conn,
		handler:         handler,
		streams:         make(map[string]*Stream),
		pendingRequests: make(map[string]chan *protocol.ResponsePayload),
		closeCh:         make(chan struct{}),
	}
}

func (m *Multiplexer) Start() {
	m.wg.Add(1)
	go m.readLoop()
}

func (m *Multiplexer) readLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.closeCh:
			return
		default:
			msg, err := protocol.ReadMessage(m.conn)
			if err != nil {
				logger.Log.Debug("Read loop exiting", zap.Error(err))
				return
			}

			if msg.Type == protocol.MsgResponse {
				var response protocol.ResponsePayload
				if err := json.Unmarshal(msg.Payload, &response); err == nil {
					m.HandleResponse(&response)
				}
			}
		}
	}
}

func (m *Multiplexer) CreateStream() *Stream {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()

	streamID := uuid.New().String()
	stream := NewStream(streamID, m)
	m.streams[streamID] = stream
	return stream
}

func (m *Multiplexer) SendRequest(streamID string, req *protocol.RequestPayload) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	msg := &protocol.Message{
		Type:    protocol.MsgRequest,
		ID:      streamID,
		Payload: data,
	}

	return protocol.WriteMessage(m.conn, msg)
}

func (m *Multiplexer) HandleResponse(resp *protocol.ResponsePayload) {
	m.pendingMu.RLock()
	ch, exists := m.pendingRequests[resp.RequestID]
	m.pendingMu.RUnlock()

	if exists {
		select {
		case ch <- resp:
		default:
			logger.Log.Warn("Response channel full",
				zap.String("request_id", resp.RequestID))
		}
	}
}

func (m *Multiplexer) RegisterPendingRequest(requestID string) chan *protocol.ResponsePayload {
	ch := make(chan *protocol.ResponsePayload, 1)
	m.pendingMu.Lock()
	m.pendingRequests[requestID] = ch
	m.pendingMu.Unlock()
	return ch
}

func (m *Multiplexer) UnregisterPendingRequest(requestID string) {
	m.pendingMu.Lock()
	delete(m.pendingRequests, requestID)
	m.pendingMu.Unlock()
}

func (m *Multiplexer) CloseStream(streamID string) {
	m.streamMu.Lock()
	delete(m.streams, streamID)
	m.streamMu.Unlock()

	if m.handler != nil {
		m.handler.OnStreamClosed(streamID)
	}
}

func (m *Multiplexer) Close() {
	close(m.closeCh)
	m.wg.Wait()

	m.streamMu.Lock()
	for _, stream := range m.streams {
		stream.Close()
	}
	m.streamMu.Unlock()
}

func (m *Multiplexer) GetGatewayCallback(requestID string) (func(*protocol.ResponsePayload), bool) {
	ch := m.RegisterPendingRequest(requestID)
	return func(resp *protocol.ResponsePayload) {
		select {
		case ch <- resp:
		default:
		}
	}, true
}
