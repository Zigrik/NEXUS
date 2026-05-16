package control

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"nexus/internal/config"
	"nexus/pkg/logger"
	"nexus/pkg/protocol"

	"go.uber.org/zap"
)

type ControlServer struct {
	config    *config.ControlConfig
	listener  net.Listener
	sessions  map[string]*Session
	sessionMu sync.RWMutex
	closeCh   chan struct{}
	wg        sync.WaitGroup
}

func NewControlServer(cfg *config.ControlConfig) *ControlServer {
	return &ControlServer{
		config:   cfg,
		sessions: make(map[string]*Session),
		closeCh:  make(chan struct{}),
	}
}

func (s *ControlServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.listener = listener

	logger.Log.Info("Control server started",
		zap.String("address", addr))

	s.wg.Add(1)
	go s.acceptLoop()

	s.wg.Add(1)
	go s.heartbeatChecker()

	return nil
}

func (s *ControlServer) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closeCh:
				return
			default:
				logger.Log.Error("Failed to accept connection", zap.Error(err))
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

func (s *ControlServer) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	msg, err := protocol.ReadMessage(conn)
	if err != nil {
		logger.Log.Error("Failed to read register message", zap.Error(err))
		return
	}

	if msg.Type != protocol.MsgRegister {
		logger.Log.Error("Expected register message", zap.Uint8("type", uint8(msg.Type)))
		return
	}

	var registerPayload protocol.RegisterPayload
	if err := json.Unmarshal(msg.Payload, &registerPayload); err != nil {
		logger.Log.Error("Failed to parse register payload", zap.Error(err))
		return
	}

	s.sessionMu.Lock()
	if oldSession, exists := s.sessions[registerPayload.DeviceID]; exists {
		logger.Log.Warn("Device reconnecting, closing old session",
			zap.String("device_id", registerPayload.DeviceID))
		oldSession.Close()
	}

	session := NewSession(registerPayload.DeviceID, conn)
	s.sessions[registerPayload.DeviceID] = session
	s.sessionMu.Unlock()

	logger.Log.Info("Device registered",
		zap.String("device_id", registerPayload.DeviceID),
		zap.String("device_name", registerPayload.DeviceName),
		zap.String("version", registerPayload.Version))

	ackPayload := protocol.RegisterAckPayload{
		Status:  "ok",
		Message: "Registration successful",
	}
	ackData, _ := json.Marshal(ackPayload)
	ackMsg := &protocol.Message{
		Type:    protocol.MsgRegisterAck,
		Payload: ackData,
	}

	if err := protocol.WriteMessage(conn, ackMsg); err != nil {
		logger.Log.Error("Failed to send registration ack", zap.Error(err))
		return
	}

	for {
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			logger.Log.Info("Device disconnected",
				zap.String("device_id", registerPayload.DeviceID),
				zap.Error(err))
			break
		}

		switch msg.Type {
		case protocol.MsgHeartbeat:
			session.UpdateHeartbeat()
			logger.Log.Debug("Heartbeat received",
				zap.String("device_id", registerPayload.DeviceID))

		case protocol.MsgResponse:
			var response protocol.ResponsePayload
			if err := json.Unmarshal(msg.Payload, &response); err != nil {
				logger.Log.Error("Failed to parse response", zap.Error(err))
				continue
			}
			session.HandleResponse(&response)

		case protocol.MsgClose:
			logger.Log.Info("Device closing connection",
				zap.String("device_id", registerPayload.DeviceID))
			return

		default:
			logger.Log.Warn("Unknown message type",
				zap.Uint8("type", uint8(msg.Type)))
		}
	}

	s.sessionMu.Lock()
	delete(s.sessions, registerPayload.DeviceID)
	s.sessionMu.Unlock()
	session.Close()
}

func (s *ControlServer) heartbeatChecker() {
	defer s.wg.Done()

	ticker := time.NewTicker(time.Duration(s.config.HeartbeatInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.closeCh:
			return
		case <-ticker.C:
			s.checkHeartbeats()
		}
	}
}

func (s *ControlServer) checkHeartbeats() {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()

	for deviceID, session := range s.sessions {
		if !session.IsAlive(s.config.HeartbeatTimeout) {
			logger.Log.Warn("Device heartbeat timeout",
				zap.String("device_id", deviceID))
			go session.Close()
			delete(s.sessions, deviceID)
		} else {
			go session.SendHeartbeat()
		}
	}
}

func (s *ControlServer) GetSession(deviceID string) (*Session, bool) {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	session, exists := s.sessions[deviceID]
	return session, exists
}

func (s *ControlServer) Stop() error {
	logger.Log.Info("Stopping control server...")
	close(s.closeCh)

	if s.listener != nil {
		s.listener.Close()
	}

	s.sessionMu.Lock()
	for _, session := range s.sessions {
		session.Close()
	}
	s.sessionMu.Unlock()

	s.wg.Wait()
	logger.Log.Info("Control server stopped")
	return nil
}
