package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bbmeme05/device-ingress/internal/batcher"
	"github.com/bbmeme05/device-ingress/internal/httpapi"
	"github.com/bbmeme05/device-ingress/internal/queuebridgepb"
)

type config struct {
	ListenAddress      string
	QueueBridgeAddress string
	QueueTopic         string
	MaxBodyBytes       int64
	QueueDepth         int
	BatchSize          int
	BatchWait          time.Duration
	PushTimeout        time.Duration
	PushWorkers        int
	PushRetries        int
}

func main() {
	config, err := loadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	publisher, err := queuebridgepb.NewPublisher(config.QueueBridgeAddress, config.QueueTopic)
	if err != nil {
		slog.Error("create queue bridge publisher", "error", err)
		os.Exit(1)
	}
	defer publisher.Close()

	batch := batcher.New(publisher, batcher.Config{
		Workers:     config.PushWorkers,
		QueueDepth:  config.QueueDepth,
		BatchSize:   config.BatchSize,
		BatchWait:   config.BatchWait,
		PushTimeout: config.PushTimeout,
		Retries:     config.PushRetries,
	})
	defer batch.Close()

	api := httpapi.New(httpapi.Config{
		MaxCompressedBodyBytes: config.MaxBodyBytes,
		MaxDecodedBodyBytes:    config.MaxBodyBytes,
	}, batch)

	server := &http.Server{
		Addr:              config.ListenAddress,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), api.ShutdownTimeout())
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}()

	slog.Info("device ingress listening", "address", config.ListenAddress, "queue_topic", config.QueueTopic)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("http server failed", "error", err)
		os.Exit(1)
	}
}

func loadConfig() (config, error) {
	config := config{
		ListenAddress:      envString("LISTEN_ADDR", ":8080"),
		QueueBridgeAddress: envString("QUEUE_BRIDGE_ADDR", "queue-bridge:50051"),
		QueueTopic:         envString("QUEUE_TOPIC", "device"),
		MaxBodyBytes:       envInt64("MAX_BODY_BYTES", 1<<20),
		QueueDepth:         envInt("QUEUE_DEPTH", 32768),
		BatchSize:          envInt("BATCH_SIZE", 500),
		BatchWait:          envDuration("BATCH_WAIT", 2*time.Millisecond),
		PushTimeout:        envDuration("PUSH_TIMEOUT", 2*time.Second),
		PushWorkers:        envInt("PUSH_WORKERS", 4),
		PushRetries:        envInt("PUSH_RETRIES", 2),
	}
	if config.MaxBodyBytes <= 0 || config.QueueDepth <= 0 || config.BatchSize <= 0 || config.BatchWait <= 0 || config.PushTimeout <= 0 || config.PushWorkers <= 0 || config.PushRetries < 0 {
		return config, fmt.Errorf("numeric configuration values must be positive (PUSH_RETRIES may be zero)")
	}
	return config, nil
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(envString(key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}

func envInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(envString(key, strconv.FormatInt(fallback, 10)), 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(envString(key, fallback.String()))
	if err != nil {
		return fallback
	}
	return value
}
