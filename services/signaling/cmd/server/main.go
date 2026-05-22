package main

import (
	"context"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/signaling/internal/auth"
	"github.com/vks4/vks4/services/signaling/internal/config"
	"github.com/vks4/vks4/services/signaling/internal/mediarpc"
	"github.com/vks4/vks4/services/signaling/internal/presence"
	"github.com/vks4/vks4/services/signaling/internal/session"
	"github.com/vks4/vks4/services/signaling/internal/ws"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := config.FromEnv()
	log, _ := zap.NewProduction()
	defer log.Sync()

	verifier, err := auth.NewVerifier(cfg.JWTPubKeyPath, cfg.JWTIssuer, cfg.JWTAudience)
	if err != nil {
		log.Fatal("jwt", zap.Error(err))
	}
	// Redis для presence (best-effort: при сбое идём дальше, без participants в admin)
	var pres *presence.Store
	if opt, err := redis.ParseURL(cfg.RedisURL); err == nil {
		rdb := redis.NewClient(opt)
		if err := rdb.Ping(ctx).Err(); err == nil {
			pres = presence.New(rdb)
		} else {
			log.Warn("redis ping (presence disabled)", zap.Error(err))
		}
	} else {
		log.Warn("redis url parse", zap.Error(err))
	}

	hub := session.NewHub()
	media := mediarpc.NewHTTPClient(cfg.MediaWorkerHTTP, log)
	wsh := ws.NewHandler(hub, media, verifier, pres, log)

	bch := ws.NewBroadcastHandler(hub, log)
	mux := http.NewServeMux()
	mux.Handle("/ws", wsh)
	mux.Handle("/broadcast", bch)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
	mSrv := &http.Server{Addr: cfg.MetricsAddr, Handler: metricsMux}

	go func() {
		log.Info("ws listening", zap.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("http", zap.Error(err))
		}
	}()
	go func() {
		if err := mSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Warn("metrics", zap.Error(err))
		}
	}()

	<-ctx.Done()
	shut, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = srv.Shutdown(shut)
	_ = mSrv.Shutdown(shut)
}
