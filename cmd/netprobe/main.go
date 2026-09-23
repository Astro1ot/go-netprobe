package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	probev1 "github.com/Astro1ot/go-netprobe/gen/probe/v1"
	"github.com/Astro1ot/go-netprobe/internal/api"
	"github.com/Astro1ot/go-netprobe/internal/probe"
	"google.golang.org/grpc"
)

func main() {
	config := flag.String("config", "configs/targets.local.json", "target allow-list JSON")
	workers := flag.Int("workers", 4, "global number of concurrent network checks")
	timeout := flag.Duration("timeout", 2*time.Second, "timeout for one target")
	health := flag.Bool("healthcheck", false, "check local health endpoint")
	flag.Parse()
	if *health {
		client := http.Client{Timeout: time.Second}
		response, err := client.Get("http://127.0.0.1:8080/healthz")
		if err != nil {
			os.Exit(1)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if err := run(*config, *workers, *timeout); err != nil {
		slog.Error("netprobe stopped", "error", err)
		os.Exit(1)
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func run(path string, workers int, timeout time.Duration) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var targets []probe.Target
	if err := json.Unmarshal(data, &targets); err != nil {
		return err
	}
	engine, err := probe.New(targets, probe.Options{Concurrency: workers, MaxBatches: 8, Timeout: timeout}, nil)
	if err != nil {
		return err
	}
	httpListener, err := net.Listen("tcp", env("HTTP_ADDR", "127.0.0.1:8082"))
	if err != nil {
		return err
	}
	defer httpListener.Close()
	grpcListener, err := net.Listen("tcp", env("GRPC_ADDR", "127.0.0.1:9092"))
	if err != nil {
		return err
	}
	defer grpcListener.Close()
	httpServer := &http.Server{Handler: api.HTTP(engine), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	grpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(16<<10), grpc.MaxConcurrentStreams(32))
	probev1.RegisterProbeServiceServer(grpcServer, &api.GRPC{Engine: engine})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 2)
	go func() { errs <- httpServer.Serve(httpListener) }()
	go func() { errs <- grpcServer.Serve(grpcListener) }()
	slog.Info("netprobe listening", "http", httpListener.Addr(), "grpc", grpcListener.Addr(), "targets", len(targets), "workers", workers)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errs:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	grpcDone := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(grpcDone) }()
	httpErr := httpServer.Shutdown(shutdown)
	select {
	case <-grpcDone:
	case <-shutdown.Done():
		grpcServer.Stop()
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", serveErr)
	}
	return httpErr
}
