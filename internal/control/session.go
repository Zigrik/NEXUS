package control

import (
	"encoding/json"
	"net"
	"sync"
	"time"

	"nexus/pkg/logger"
	"nexus/pkg/protocol"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Session struct {
	ID            string
	DeviceID      string
	Conn          net.Conn
	LastHeartbeat time.Time
	mu            sync.RWMutex
	closeCh       chan struct{}
	responseChan  chan *protocol.ResponsePayload
}

func NewSession(deviceID string, conn net.Conn) *Session {
	return &Session{
		ID:            uuid.New().String(),
		DeviceID:      deviceID,
		Conn:          conn,
		LastHeartbeat: time.Now(),
		closeCh:       make(chan struct{}),
		responseChan:  make(chan *protocol.ResponsePayload, 100),
	}
}

func (s *Session) UpdateHeartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastHeartbeat = time.Now()
}

func (s *Session) IsAlive(timeout int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.LastHeartbeat).Seconds() < float64(timeout)
}

func (s *Session) SendMessage(msg *protocol.Message) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return protocol.WriteMessage(s.Conn, msg)
}

func (s *Session) SendRequest(req *protocol.RequestPayload) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}

	msg := &protocol.Message{
		Type:    protocol.MsgRequest,
		ID:      req.RequestID,
		Payload: payload,
	}

	return s.SendMessage(msg)
}

func (s *Session) SendHeartbeat() error {
	heartbeat := protocol.HeartbeatPayload{
		Timestamp: time.Now().Unix(),
	}
	payload, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}

	msg := &protocol.Message{
		Type:    protocol.MsgHeartbeat,
		Payload: payload,
	}

	return s.SendMessage(msg)
}

func (s *Session) GetResponseChan() <-chan *protocol.ResponsePayload {
	return s.responseChan
}

func (s *Session) HandleResponse(resp *protocol.ResponsePayload) {
	select {
	case s.responseChan <- resp:
	case <-time.After(time.Second):
		logger.Log.Warn("Response channel full, dropping response",
			zap.String("request_id", resp.RequestID))
	}
}

func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case <-s.closeCh:
		return
	default:
		close(s.closeCh)
	}

	if s.Conn != nil {
		closeMsg := &protocol.Message{
			Type: protocol.MsgClose,
		}
		_ = protocol.WriteMessage(s.Conn, closeMsg)
		s.Conn.Close()
	}

	close(s.responseChan)

	logger.Log.Info("Session closed",
		zap.String("device_id", s.DeviceID),
		zap.String("session_id", s.ID))
}
