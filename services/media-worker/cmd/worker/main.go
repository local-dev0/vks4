package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/media-worker/internal/config"
	"github.com/vks4/vks4/services/media-worker/internal/httpapi"
	"github.com/vks4/vks4/services/media-worker/internal/pipeline"
	"github.com/vks4/vks4/services/media-worker/internal/room"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := config.FromEnv()
	log, _ := zap.NewProduction()
	defer log.Sync()

	// ICE
	ice := []webrtc.ICEServer{}
	if len(cfg.STUNURLs) > 0 {
		ice = append(ice, webrtc.ICEServer{URLs: cfg.STUNURLs})
	}
	if len(cfg.TURNURLs) > 0 {
		ice = append(ice, webrtc.ICEServer{
			URLs:       cfg.TURNURLs,
			Username:   cfg.TURNUsername,
			Credential: cfg.TURNPassword,
		})
	}

	// Pipeline factory + registry
	pipes := pipeline.NewRegistry(pipeline.NewGstFactory())

	rooms := room.NewRegistry(pipes, log, ice, cfg.VideoCodec,
		cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS, cfg.VideoBitrate, cfg.AudioBitrate, cfg.SFU,
		cfg.PublicIP, uint16(cfg.UDPPortMin), uint16(cfg.UDPPortMax))

	api := &httpapi.API{Rooms: rooms, RecordingDir: cfg.RecordingDir, Log: log}
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Router()}

	// Heartbeat в Redis (для media-router)
	if rOpt, err := redis.ParseURL(cfg.RedisURL); err == nil {
		rdb := redis.NewClient(rOpt)
		go heartbeat(ctx, rdb, cfg.WorkerID, cfg.HTTPAddr, rooms, log)
	}

	// metrics
	mm := http.NewServeMux()
	mm.Handle("/metrics", promhttp.Handler())
	mSrv := &http.Server{Addr: cfg.MetricsAddr, Handler: mm}

	go func() {
		log.Info("media-worker http listening", zap.String("addr", cfg.HTTPAddr))
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

func heartbeat(ctx context.Context, rdb *redis.Client, id, addr string, rooms *room.Registry, log *zap.Logger) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			worker := map[string]any{
				"id":       id,
				"grpcAddr": addr,
				"rooms":    rooms.Count(),
				"lastSeen": time.Now().Format(time.RFC3339Nano),
				"version":  "0.1.0",
			}
			b, _ := json.Marshal(worker)
			if err := rdb.Set(ctx, "mcu:worker:"+id, b, 30*time.Second).Err(); err != nil {
				log.Warn("heartbeat", zap.Error(err))
			}
		}
	}
}
