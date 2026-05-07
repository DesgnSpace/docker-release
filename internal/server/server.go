package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/malico/docker-release/internal/controller"
	"github.com/malico/docker-release/internal/state"
)

type Config struct {
	BindAddr   string
	APIEnabled bool
	APIPort    int
	WebEnabled bool
	WebPort    int
	Version    string
}

func ConfigFromEnv() Config {
	cfg := Config{
		BindAddr: envOr("DR_BIND_ADDR", "0.0.0.0"),
		APIPort:  envIntOr("DR_API_PORT", 9080),
		WebPort:  envIntOr("DR_WEB_PORT", 9081),
	}
	cfg.APIEnabled = envBool("DR_EXPOSE_API")
	cfg.WebEnabled = envBool("DR_EXPOSE_WEB")
	return cfg
}

type Server struct {
	cfg     Config
	ctrl    *controller.Controller
	mgr     *state.Manager
	project string
}

func New(cfg Config, ctrl *controller.Controller, mgr *state.Manager, project string) *Server {
	return &Server{
		cfg:     cfg,
		ctrl:    ctrl,
		mgr:     mgr,
		project: project,
	}
}

func (s *Server) Start(ctx context.Context) error {
	var wg sync.WaitGroup

	if s.cfg.APIEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.listenAndServe(ctx, s.cfg.APIPort, s.apiMux())
		}()
	}

	if s.cfg.WebEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.listenAndServe(ctx, s.cfg.WebPort, s.webMux())
		}()
	}

	wg.Wait()
	return nil
}

func (s *Server) listenAndServe(ctx context.Context, port int, mux http.Handler) {
	addr := fmt.Sprintf("%s:%d", s.cfg.BindAddr, port)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("[server] shutdown %s: %v", addr, err)
		}
	}()

	log.Printf("[server] listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("[server] %s: %v", addr, err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string) bool {
	v := os.Getenv(key)
	return v == "1" || v == "true" || v == "yes"
}
