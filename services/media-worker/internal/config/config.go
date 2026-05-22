package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	WorkerID        string
	HTTPAddr        string
	GRPCAddr        string
	MetricsAddr     string
	RedisURL        string
	RecordingDir    string
	S3Enabled       bool
	VideoBitrate    int
	AudioBitrate    int
	VideoWidth      int
	VideoHeight     int
	VideoFPS        int
	VideoCodec      string
	STUNURLs        []string
	TURNURLs        []string
	TURNUsername    string
	TURNPassword    string
	SFU             bool
	PublicIP        string // публичный IP, подставляется как host-кандидат (1:1 NAT)
	UDPPortMin      int
	UDPPortMax      int
}

func FromEnv() *Config {
	return &Config{
		WorkerID:     env("MEDIA_WORKER_ID", "worker-1"),
		HTTPAddr:     env("MEDIA_WORKER_HTTP", ":9091"),
		GRPCAddr:     env("MEDIA_WORKER_GRPC", ":9095"),
		MetricsAddr:  env("PROMETHEUS_HTTP", ":9100"),
		RedisURL:     env("REDIS_URL", "redis://redis:6379/0"),
		RecordingDir: env("RECORDING_DIR", "/data/recordings"),
		S3Enabled:    boolEnv("RECORDING_S3_ENABLED", true),
		VideoBitrate: intEnv("MEDIA_WORKER_VIDEO_BITRATE", 2_500_000),
		AudioBitrate: intEnv("MEDIA_WORKER_AUDIO_BITRATE", 64_000),
		VideoWidth:   intEnv("MEDIA_WORKER_VIDEO_WIDTH", 1280),
		VideoHeight:  intEnv("MEDIA_WORKER_VIDEO_HEIGHT", 720),
		VideoFPS:     intEnv("MEDIA_WORKER_VIDEO_FPS", 30),
		VideoCodec:   env("MEDIA_WORKER_VIDEO_CODEC", "vp8"),
		STUNURLs:     splitList(env("STUN_URLS", "stun:coturn:3478")),
		TURNURLs:     splitList(env("TURN_URLS", "")),
		TURNUsername: env("TURN_USERNAME", ""),
		TURNPassword: env("TURN_PASSWORD", ""),
		SFU:          boolEnv("MEDIA_WORKER_MODE_SFU", true), // smoke режим по умолчанию
		PublicIP:     env("MEDIA_WORKER_PUBLIC_IP", ""),
		UDPPortMin:   intEnv("MEDIA_WORKER_UDP_MIN", 50000),
		UDPPortMax:   intEnv("MEDIA_WORKER_UDP_MAX", 50050),
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func intEnv(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return d
}

func boolEnv(k string, d bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return d
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
