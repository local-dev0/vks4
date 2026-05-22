package config

import (
	"os"
	"time"
)

type Config struct {
	HTTPAddr        string
	GRPCAddr        string
	MetricsAddr     string
	RedisURL        string
	JWTPubKeyPath   string
	JWTIssuer       string
	JWTAudience     string
	MediaWorkerHTTP string
	WSReadDeadline  time.Duration
	PingInterval    time.Duration
}

func FromEnv() *Config {
	return &Config{
		HTTPAddr:        env("SIGNALING_HTTP", ":8081"),
		GRPCAddr:        env("SIGNALING_GRPC", ":9092"),
		MetricsAddr:     env("PROMETHEUS_HTTP", ":9100"),
		RedisURL:        env("REDIS_URL", "redis://redis:6379/0"),
		JWTPubKeyPath:   env("JWT_PUBLIC_KEY_PATH", "/run/secrets/jwt_public.pem"),
		JWTIssuer:       env("JWT_ISSUER", "vks4"),
		JWTAudience:     env("JWT_AUDIENCE", "vks4-admin"),
		MediaWorkerHTTP: env("MEDIA_WORKER_HTTP_URL", "http://media-worker:9091"),
		WSReadDeadline:  90 * time.Second,
		PingInterval:    25 * time.Second,
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
