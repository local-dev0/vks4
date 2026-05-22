package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/sip-gateway/internal/bridge"
	"github.com/vks4/vks4/services/sip-gateway/internal/esl"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log, _ := zap.NewProduction()
	defer log.Sync()

	br := bridge.New(log)

	// FreeSWITCH ESL (опционально — если хост недоступен, gateway работает в standalone-стабе).
	host := env("FS_ESL_HOST", "")
	if host != "" {
		port, _ := strconv.Atoi(env("FS_ESL_PORT", "8021"))
		password := env("FS_ESL_PASSWORD", "ClueCon")
		go func() {
			for {
				c, err := esl.Dial(ctx, host, port, password, log)
				if err != nil {
					log.Warn("esl dial", zap.Error(err))
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Second):
					}
					continue
				}
				log.Info("esl connected", zap.String("host", host))
				_ = c.Subscribe("CHANNEL_CREATE", "CHANNEL_DESTROY", "CHANNEL_HANGUP")
				// Здесь должен быть event loop: при CHANNEL_CREATE → bridge.Accept,
				// при CHANNEL_DESTROY → bridge.Release. Реализация в v0.2.
				<-ctx.Done()
				_ = c.Close()
				return
			}
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/calls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(br.Active())
	})

	mm := http.NewServeMux()
	mm.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{Addr: env("SIP_GATEWAY_HTTP", ":9094"), Handler: mux}
	mSrv := &http.Server{Addr: env("PROMETHEUS_HTTP", ":9100"), Handler: mm}

	go func() {
		log.Info("sip-gateway http listening", zap.String("addr", srv.Addr))
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
