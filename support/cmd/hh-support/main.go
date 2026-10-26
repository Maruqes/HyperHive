package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hyperhive/support/internal/config"
	"hyperhive/support/internal/httpapi"
	"hyperhive/support/internal/realtime"
	"hyperhive/support/internal/service"
	"hyperhive/support/internal/storage/sqlite"
)

func main() {
	health := flag.Bool("healthcheck", false, "check the running service and exit")
	flag.Parse()
	if *health {
		cfg, err := config.Load()
		if err == nil {
			err = healthcheck(cfg.Listen)
		}
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := sqlite.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	hub := realtime.NewHub()
	application, err := service.New(store, hub, nil)
	if err != nil {
		store.Close()
		return err
	}
	defer application.Close()
	handler, err := httpapi.New(application, hub, cfg.ProxySecret)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	monitorDone := make(chan struct{})
	go func() { defer close(monitorDone); application.Run(ctx) }()
	server := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	log.Printf("HH Support listening on %s", cfg.Listen)
	select {
	case <-ctx.Done():
	case err = <-failures:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if shutdownErr := server.Shutdown(shutdown); shutdownErr != nil {
		server.Close()
		if err == nil {
			err = shutdownErr
		}
	}
	stop()
	<-monitorDone
	return err
}
