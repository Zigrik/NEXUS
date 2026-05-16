package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nexus/internal/config"
	"nexus/internal/control"
	"nexus/pkg/logger"
	"nexus/pkg/protocol"

	"go.uber.org/zap"
)

type GatewayServer struct {
	config     *config.GatewayConfig
	control    *control.ControlServer
	routes     map[string]*config.RouteConfig
	httpServer *http.Server
}

func NewGatewayServer(cfg *config.GatewayConfig, control *control.ControlServer, routes []config.RouteConfig) *GatewayServer {
	routeMap := make(map[string]*config.RouteConfig)
	for i := range routes {
		routeMap[routes[i].Host] = &routes[i]
	}

	return &GatewayServer{
		config:  cfg,
		control: control,
		routes:  routeMap,
	}
}

func (g *GatewayServer) Start() error {
	addr := fmt.Sprintf("%s:%d", g.config.Host, g.config.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", g.handleProxy)
	mux.HandleFunc("/health", g.handleHealth)
	mux.HandleFunc("/routes", g.handleRoutes)
	mux.HandleFunc("/devices", g.handleDevices)

	g.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  time.Duration(g.config.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(g.config.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(g.config.IdleTimeout) * time.Second,
	}

	logger.Log.Info("HTTP gateway started", zap.String("address", addr))

	go func() {
		if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Error("HTTP gateway failed", zap.Error(err))
		}
	}()

	return nil
}

func (g *GatewayServer) handleProxy(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	route, exists := g.routes[host]
	if !exists {
		logger.Log.Warn("No route found for host", zap.String("host", host))
		http.Error(w, "No route configured for this host", http.StatusNotFound)
		return
	}

	session, exists := g.control.GetSession(route.DeviceID)
	if !exists {
		logger.Log.Warn("Device not connected",
			zap.String("device_id", route.DeviceID),
			zap.String("host", host))
		http.Error(w, "Device is not connected", http.StatusServiceUnavailable)
		return
	}

	requestID := generateRequestID()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Log.Error("Failed to read request body", zap.Error(err))
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	reqPayload := &protocol.RequestPayload{
		RequestID:  requestID,
		Method:     r.Method,
		URL:        r.URL.String(),
		Headers:    headers,
		Body:       body,
		TargetPort: route.TargetPort,
	}

	if err := session.SendRequest(reqPayload); err != nil {
		logger.Log.Error("Failed to send request to device",
			zap.String("device_id", route.DeviceID),
			zap.Error(err))
		http.Error(w, "Failed to send request", http.StatusBadGateway)
		return
	}

	select {
	case resp := <-session.GetResponseChan():
		if resp.RequestID != requestID {
			logger.Log.Warn("Response ID mismatch",
				zap.String("expected", requestID),
				zap.String("got", resp.RequestID))
			return
		}
		g.writeResponse(w, resp)
	case <-time.After(60 * time.Second):
		logger.Log.Error("Request timeout",
			zap.String("request_id", requestID),
			zap.String("host", host))
		http.Error(w, "Request timeout", http.StatusGatewayTimeout)
	}
}

func (g *GatewayServer) writeResponse(w http.ResponseWriter, resp *protocol.ResponsePayload) {
	if resp.Error != "" {
		logger.Log.Error("Device returned error",
			zap.String("request_id", resp.RequestID),
			zap.String("error", resp.Error))
		http.Error(w, resp.Error, http.StatusBadGateway)
		return
	}

	for key, value := range resp.Headers {
		w.Header().Set(key, value)
	}

	w.WriteHeader(resp.StatusCode)

	if len(resp.Body) > 0 {
		if _, err := w.Write(resp.Body); err != nil {
			logger.Log.Error("Failed to write response body", zap.Error(err))
		}
	}

	logger.Log.Debug("Request proxied successfully",
		zap.String("request_id", resp.RequestID),
		zap.Int("status_code", resp.StatusCode))
}

func (g *GatewayServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Unix(),
	})
}

func (g *GatewayServer) handleRoutes(w http.ResponseWriter, r *http.Request) {
	routes := make([]map[string]interface{}, 0)
	for host, route := range g.routes {
		routes = append(routes, map[string]interface{}{
			"host":        host,
			"device_id":   route.DeviceID,
			"target_port": route.TargetPort,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"routes": routes,
	})
}

func (g *GatewayServer) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices := make([]map[string]interface{}, 0)

	// This would need a method to list sessions from control server
	// For now, return empty list

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"devices": devices,
	})
}

func (g *GatewayServer) Stop() error {
	logger.Log.Info("Stopping HTTP gateway...")
	if g.httpServer != nil {
		return g.httpServer.Close()
	}
	return nil
}

func generateRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
