package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/media-router/internal/dispatcher"
	"github.com/vks4/vks4/services/media-router/internal/registry"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log, _ := zap.NewProduction()
	defer log.Sync()

	redisURL := env("REDIS_URL", "redis://redis:6379/0")
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatal("redis url", zap.Error(err))
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("redis ping", zap.Error(err))
	}

	reg := registry.New(rdb, 30*time.Second)
	disp := dispatcher.New(reg, 24*time.Hour)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/pick", func(w http.ResponseWriter, r *http.Request) {
		room := r.URL.Query().Get("roomId")
		if room == "" {
			http.Error(w, "roomId required", http.StatusBadRequest)
			return
		}
		worker, err := disp.Pick(r.Context(), room)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(worker)
	})
	mux.HandleFunc("/workers", func(w http.ResponseWriter, r *http.Request) {
		list, err := reg.List(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	})

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{Addr: env("MEDIA_ROUTER_HTTP", ":9090"), Handler: mux}
	mSrv := &http.Server{Addr: env("PROMETHEUS_HTTP", ":9100"), Handler: metricsMux}

	go func() {
		log.Info("router listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("http", zap.Error(err))
		}
	}()
	go func() { _ = mSrv.ListenAndServe() }()

	<-ctx.Done()
	shut, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = srv.Shutdown(shut)
	_ = mSrv.Shutdown(shut)
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
