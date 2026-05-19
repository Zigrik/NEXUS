package gateway

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nexus/internal/config"
	"nexus/internal/control"
	"nexus/internal/static"
	"nexus/pkg/logger"
	"nexus/pkg/protocol"

	"go.uber.org/zap"
)

type GatewayServer struct {
	config        *config.GatewayConfig
	control       *control.ControlServer
	routes        map[string]*config.RouteConfig
	staticHandler *static.StaticHandler
	httpServer    *http.Server
}

func NewGatewayServer(cfg *config.GatewayConfig, control *control.ControlServer, routes []config.RouteConfig, staticCfg *config.StaticConfig) *GatewayServer {
	routeMap := make(map[string]*config.RouteConfig)
	for i := range routes {
		routeMap[routes[i].Host] = &routes[i]
	}

	var staticHandler *static.StaticHandler
	if staticCfg.Enabled {
		staticHandler = static.NewStaticHandler(staticCfg.Domain, staticCfg.Path, staticCfg.IndexFile)
	}

	return &GatewayServer{
		config:        cfg,
		control:       control,
		routes:        routeMap,
		staticHandler: staticHandler,
	}
}

func (g *GatewayServer) Start() error {
	addr := fmt.Sprintf("%s:%d", g.config.Host, g.config.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", g.handleHealth)
	mux.HandleFunc("/routes", g.handleRoutes)
	mux.HandleFunc("/devices", g.handleDevices)
	mux.HandleFunc("/", g.handleRequest)

	g.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  time.Duration(g.config.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(g.config.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(g.config.IdleTimeout) * time.Second,
	}

	if g.config.HTTPS {
		g.httpServer.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1"},
		}

		logger.Log.Info("HTTPS gateway started",
			zap.String("address", addr),
			zap.String("cert", g.config.CertFile))

		go func() {
			if err := g.httpServer.ListenAndServeTLS(g.config.CertFile, g.config.KeyFile); err != nil && err != http.ErrServerClosed {
				logger.Log.Error("HTTPS gateway failed", zap.Error(err))
			}
		}()
	} else {
		logger.Log.Info("HTTP gateway started", zap.String("address", addr))
		go func() {
			if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Log.Error("HTTP gateway failed", zap.Error(err))
			}
		}()
	}

	return nil
}

func (g *GatewayServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	if g.staticHandler != nil && host == g.staticHandler.Domain() {
		g.staticHandler.ServeHTTP(w, r)
		return
	}

	route, exists := g.routes[host]
	if !exists {
		logger.Log.Warn("No route found for host",
			zap.String("host", host),
			zap.String("path", r.URL.Path))
		http.Error(w, fmt.Sprintf("No route configured for host: %s", host), http.StatusNotFound)
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

	headers["X-Forwarded-Host"] = host
	headers["X-Forwarded-Proto"] = "https"

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
		if key == "Content-Length" {
			continue
		}
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
		"status":  "ok",
		"time":    time.Now().Unix(),
		"version": "1.0.0",
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
		"count":  len(routes),
	})
}

func (g *GatewayServer) handleDevices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"devices": []string{},
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
