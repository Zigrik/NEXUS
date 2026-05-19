package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nexus/internal/config"
	"nexus/internal/control"
	"nexus/internal/gateway"
	"nexus/pkg/logger"

	"go.uber.org/zap"
)

func main() {
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		panic("Failed to load config: " + err.Error())
	}

	if err := logger.Init(cfg.Log.Level, cfg.Log.Encoding); err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
	defer logger.Sync()

	logger.Log.Info("Starting Nexus VPS server",
		zap.String("version", "1.0.0"),
		zap.String("config", cfgPath))

	controlServer := control.NewControlServer(&cfg.Control)
	if err := controlServer.Start(); err != nil {
		logger.Log.Fatal("Failed to start control server", zap.Error(err))
	}

	gatewayServer := gateway.NewGatewayServer(&cfg.Gateway, controlServer, cfg.Routes, &cfg.Static)
	if err := gatewayServer.Start(); err != nil {
		logger.Log.Fatal("Failed to start gateway server", zap.Error(err))
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Log.Info("Shutting down servers...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := gatewayServer.Stop(); err != nil {
		logger.Log.Error("Failed to stop gateway server", zap.Error(err))
	}

	if err := controlServer.Stop(); err != nil {
		logger.Log.Error("Failed to stop control server", zap.Error(err))
	}

	<-ctx.Done()
	logger.Log.Info("Server shutdown complete")
}
