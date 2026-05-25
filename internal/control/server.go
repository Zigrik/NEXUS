package control

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"
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
	tlsConfig *tls.Config
}

func NewControlServer(cfg *config.ControlConfig) *ControlServer {
	return &ControlServer{
		config:   cfg,
		sessions: make(map[string]*Session),
		closeCh:  make(chan struct{}),
	}
}

func (s *ControlServer) loadTLSConfig() error {
	if !s.config.TLSEnabled {
		logger.Log.Info("TLS disabled for control server")
		return nil
	}

	// Загружаем CA сертификат для проверки клиентов
	caCert, err := os.ReadFile(s.config.CAFile)
	if err != nil {
		return fmt.Errorf("failed to read CA cert: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return fmt.Errorf("failed to parse CA cert")
	}

	// Загружаем серверный сертификат
	cert, err := tls.LoadX509KeyPair(s.config.CertFile, s.config.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to load server cert: %w", err)
	}

	s.tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
		MinVersion:   tls.VersionTLS12,
	}

	logger.Log.Info("mTLS configured for control server",
		zap.String("ca", s.config.CAFile),
		zap.String("cert", s.config.CertFile))

	return nil
}

func (s *ControlServer) Start() error {
	// Загружаем TLS конфигурацию
	if err := s.loadTLSConfig(); err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var listener net.Listener
	var err error

	if s.config.TLSEnabled {
		listener, err = tls.Listen("tcp", addr, s.tlsConfig)
	} else {
		listener, err = net.Listen("tcp", addr)
	}

	if err != nil {
		return err
	}

	s.listener = listener

	logger.Log.Info("Control server started",
		zap.String("address", addr),
		zap.Bool("tls_enabled", s.config.TLSEnabled))

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

	var clientCN string

	// Если TLS включен, проверяем сертификат клиента
	if s.config.TLSEnabled {
		tlsConn, ok := conn.(*tls.Conn)
		if !ok {
			logger.Log.Error("Not a TLS connection")
			return
		}

		// Выполняем handshake
		if err := tlsConn.Handshake(); err != nil {
			logger.Log.Error("TLS handshake failed", zap.Error(err))
			return
		}

		// Проверяем сертификат клиента
		certs := tlsConn.ConnectionState().PeerCertificates
		if len(certs) == 0 {
			logger.Log.Error("No client certificate provided")
			return
		}

		clientCN = certs[0].Subject.CommonName
		logger.Log.Info("Client authenticated",
			zap.String("common_name", clientCN))
	}

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

	// Проверяем, что CN клиента совпадает с device_id (при mTLS)
	if s.config.TLSEnabled && clientCN != registerPayload.DeviceID {
		logger.Log.Error("Device ID mismatch",
			zap.String("cn", clientCN),
			zap.String("device_id", registerPayload.DeviceID))
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
