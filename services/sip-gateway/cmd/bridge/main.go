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
	"github.com/vks4/vks4/services/sip-gateway/internal/sipsrv"
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
				_ = c.Subscribe("CHANNEL_CREATE", "CHANNEL_ANSWER", "CHANNEL_HANGUP", "CHANNEL_DESTROY")
				events := c.Events(ctx)
			eventLoop:
				for {
					select {
					case <-ctx.Done():
						_ = c.Close()
						return
					case ev, ok := <-events:
						if !ok {
							log.Warn("esl events closed, reconnecting")
							break eventLoop
						}
						handleESLEvent(log, br, ev)
					}
				}
				_ = c.Close()
				// continue outer reconnect loop
			}
		}()
	}

	// SIP сервер для B2BUA leg от FreeSWITCH. Слушает UDP 5070, открывает RTP listener
	// на свободном порту в [17000, 17200], отвечает 200 OK с Opus SDP.
	sipBind := env("SIP_GATEWAY_BIND", ":5070")
	// Пустой/отсутствующий SIP_GATEWAY_PUBLIC_IP → sipsrv auto-detect docker-internal IP.
	sipPublic := os.Getenv("SIP_GATEWAY_PUBLIC_IP")
	rtpMin, _ := strconv.Atoi(env("SIP_GATEWAY_RTP_MIN", "17000"))
	rtpMax, _ := strconv.Atoi(env("SIP_GATEWAY_RTP_MAX", "17200"))
	sipSrv, err := sipsrv.New(sipsrv.Config{
		BindAddr:        sipBind,
		PublicIP:        sipPublic,
		RTPMinPort:      uint16(rtpMin),
		RTPMaxPort:      uint16(rtpMax),
		MediaWorkerURL:  env("MEDIA_WORKER_HTTP_URL", "http://media-worker:9091"),
		MediaWorkerHost: env("MEDIA_WORKER_HOST", "media-worker"),
		FSHost:          env("FS_ESL_HOST", ""),
	}, log, br)
	if err != nil {
		log.Fatal("sip server init", zap.Error(err))
	}
	go func() {
		if err := sipSrv.Start(ctx); err != nil {
			log.Error("sip server", zap.Error(err))
		}
	}()

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

// handleESLEvent логирует параметры SIP-звонка и (для CHANNEL_ANSWER) делает stub-вызов в bridge.
// Реальная связка peer ↔ media-worker — следующая итерация (требует Plain-RTP peer в media-worker).
func handleESLEvent(log *zap.Logger, br *bridge.Bridge, ev map[string]string) {
	name := ev["Event-Name"]
	uuid := ev["Unique-ID"]
	switch name {
	case "CHANNEL_CREATE":
		// Извлекаем X-VKS-Room — заголовок проставленный FS dialplan'ом при bridge на sip-gateway.
		// FS prefix'ит sip-заголовки как variable_sip_h_X-Vks-Room (camelize и lowercase).
		room := firstNonEmpty(
			ev["variable_sip_h_X-VKS-Room"],
			ev["variable_sip_h_X-Vks-Room"],
			ev["variable_sip_to_user"],
			ev["Caller-Destination-Number"],
		)
		caller := firstNonEmpty(
			ev["variable_sip_h_X-VKS-Caller"],
			ev["Caller-Caller-ID-Number"],
		)
		direction := ev["Call-Direction"]
		log.Info("SIP CHANNEL_CREATE",
			zap.String("uuid", uuid),
			zap.String("direction", direction),
			zap.String("caller", caller),
			zap.String("room", room),
			zap.String("dest", ev["Caller-Destination-Number"]),
		)
		if direction == "inbound" && room != "" {
			br.Accept(uuid, room, caller)
		}
	case "CHANNEL_ANSWER":
		log.Info("SIP CHANNEL_ANSWER", zap.String("uuid", uuid))
	case "CHANNEL_HANGUP", "CHANNEL_DESTROY":
		log.Info("SIP "+name,
			zap.String("uuid", uuid),
			zap.String("cause", ev["Hangup-Cause"]),
		)
		if name == "CHANNEL_DESTROY" {
			br.Release(uuid)
		}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
