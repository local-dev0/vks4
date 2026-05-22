package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env             string
	LogLevel        string
	PublicURL       string
	HTTPAddr        string
	GRPCAddr        string
	MetricsAddr     string
	DatabaseURL     string
	RedisURL        string
	MinioEndpoint   string
	MinioAccessKey  string
	MinioSecretKey  string
	MinioBucket     string
	MinioUseSSL     bool
	JWTPrivKeyPath  string
	JWTPubKeyPath   string
	JWTAccessTTL    time.Duration
	JWTRefreshTTL   time.Duration
	JWTIssuer       string
	JWTAudience     string
	BootstrapEmail  string
	BootstrapPass   string
	MediaRouterGRPC string
	SignalingGRPC   string
	SignalingHTTP   string
	MediaWorkerHTTP string
	RecordingDir    string
	RecordingS3     bool
}

func FromEnv() (*Config, error) {
	c := &Config{
		Env:             getenv("ENV", "dev"),
		LogLevel:        getenv("LOG_LEVEL", "info"),
		PublicURL:       getenv("PUBLIC_URL", "https://localhost"),
		HTTPAddr:        getenv("CONTROL_PLANE_HTTP", ":8080"),
		GRPCAddr:        getenv("CONTROL_PLANE_GRPC", ":9093"),
		MetricsAddr:     getenv("PROMETHEUS_HTTP", ":9100"),
		DatabaseURL:     mustEnv("DATABASE_URL"),
		RedisURL:        getenv("REDIS_URL", "redis://redis:6379/0"),
		MinioEndpoint:   getenv("MINIO_ENDPOINT", "minio:9000"),
		MinioAccessKey:  getenv("MINIO_ROOT_USER", "minio"),
		MinioSecretKey:  getenv("MINIO_ROOT_PASSWORD", ""),
		MinioBucket:     getenv("MINIO_BUCKET", "recordings"),
		MinioUseSSL:     getenvBool("MINIO_USE_SSL", false),
		JWTPrivKeyPath:  getenv("JWT_PRIVATE_KEY_PATH", "/run/secrets/jwt_private.pem"),
		JWTPubKeyPath:   getenv("JWT_PUBLIC_KEY_PATH", "/run/secrets/jwt_public.pem"),
		JWTAccessTTL:    getenvDuration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL:   getenvDuration("JWT_REFRESH_TTL", 30*24*time.Hour),
		JWTIssuer:       getenv("JWT_ISSUER", "vks4"),
		JWTAudience:     getenv("JWT_AUDIENCE", "vks4-admin"),
		BootstrapEmail:  getenv("BOOTSTRAP_ADMIN_EMAIL", "admin@local"),
		BootstrapPass:   getenv("BOOTSTRAP_ADMIN_PASSWORD", "admin"),
		MediaRouterGRPC: getenv("MEDIA_ROUTER_GRPC", "media-router:9090"),
		SignalingGRPC:   getenv("SIGNALING_GRPC", "signaling:9092"),
		SignalingHTTP:   getenv("SIGNALING_HTTP_URL", "http://signaling:8081"),
		MediaWorkerHTTP: getenv("MEDIA_WORKER_HTTP_URL", "http://media-worker:9091"),
		RecordingDir:    getenv("RECORDING_DIR", "/data/recordings"),
		RecordingS3:     getenvBool("RECORDING_S3_ENABLED", true),
	}
	return c, nil
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic(fmt.Sprintf("required env not set: %s", k))
	}
	return v
}

func getenvBool(k string, d bool) bool {
	if v := os.Getenv(k); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return d
}

func getenvDuration(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		dur, err := time.ParseDuration(v)
		if err == nil {
			return dur
		}
	}
	return d
}
