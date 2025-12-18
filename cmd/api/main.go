package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	application, err := app.New(ctx)
	if err != nil {
		logger.Fatal("failed to initialize app", zap.Error(err))
	}

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutdown signal received")
		cancel()
	}()

	if err := application.Run(ctx); err != nil {
		logger.Fatal("application error", zap.Error(err))
	}

	application.Close()
	logger.Info("application stopped")
}

