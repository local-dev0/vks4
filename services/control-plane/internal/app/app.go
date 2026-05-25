package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	jwtauth "github.com/vks4/vks4/services/control-plane/internal/adapters/auth/jwt"
	"github.com/vks4/vks4/services/control-plane/internal/adapters/httpapi"
	"github.com/vks4/vks4/services/control-plane/internal/adapters/mediarpc"
	"github.com/vks4/vks4/services/control-plane/internal/adapters/repo/postgres"
	redisrepo "github.com/vks4/vks4/services/control-plane/internal/adapters/repo/redisrepo"
	"github.com/vks4/vks4/services/control-plane/internal/adapters/signalingrpc"
	"github.com/vks4/vks4/services/control-plane/internal/pkg/config"
	"github.com/vks4/vks4/services/control-plane/internal/pkg/logger"
	"github.com/vks4/vks4/services/control-plane/internal/pkg/metrics"
	"github.com/vks4/vks4/services/control-plane/internal/usecase"
)

type App struct {
	cfg    *config.Config
	log    *zap.Logger
	pool   *pgxpool.Pool
	rdb    *redis.Client
	router http.Handler
	metric http.Handler
}

func rootCtx() context.Context { return context.Background() }

func Build(ctx context.Context) (*App, error) {
	cfg, err := config.FromEnv()
	if err != nil {
		return nil, err
	}
	log, err := logger.New(cfg.LogLevel, cfg.Env == "dev")
	if err != nil {
		return nil, err
	}

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	rOpt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("redis url: %w", err)
	}
	rdb := redis.NewClient(rOpt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	jwtIssuer, err := jwtauth.New(cfg.JWTPrivKeyPath, cfg.JWTPubKeyPath, cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	if err != nil {
		return nil, fmt.Errorf("jwt: %w", err)
	}

	usersRepo := postgres.NewUsers(pool)
	roomsRepo := postgres.NewRooms(pool)
	recsRepo := postgres.NewRecordings(pool)
	auditRepo := postgres.NewAudit(pool)
	refreshRepo := postgres.NewRefreshTokens(pool)
	layoutsRepo := postgres.NewLayoutTemplates(pool)
	presenceRepo := redisrepo.NewPresence(rdb)
	aliasesRepo := redisrepo.NewAliases(rdb)

	// HTTP client к media-worker для UpdateLayout/DestroyRoom/Kick. CreateRoom — no-op
	// (media-worker создаёт комнаты лениво при первом AddPeer от signaling).
	mediaClient := mediarpc.Client(mediarpc.NewHTTPClient(cfg.MediaWorkerHTTP))
	_ = cfg.MediaRouterGRPC // зарезервировано
	signalingClient := signalingrpc.New(cfg.SignalingHTTP)

	authUC := usecase.NewAuth(usersRepo, refreshRepo, auditRepo, jwtIssuer, cfg.BootstrapPass)
	usersUC := usecase.NewUsers(usersRepo, auditRepo)
	roomsUC := usecase.NewRooms(roomsRepo, presenceRepo, auditRepo, mediaClient, signalingClient, aliasesRepo, cfg.PublicURL)
	recsUC := usecase.NewRecordings(recsRepo, auditRepo, mediaClient)
	auditUC := usecase.NewAuditor(auditRepo)
	layoutsUC := usecase.NewLayoutTemplates(layoutsRepo, auditRepo)

	// Backfill room-alias map: для существующих комнат добавляем имя→UUID в Redis,
	// чтобы SIP-резолв заработал сразу после деплоя без пересоздания комнат.
	go func() {
		bctx, bcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer bcancel()
		rows, _, err := roomsRepo.List(bctx, "", 1000, 0)
		if err != nil {
			log.Warn("alias backfill list", zap.Error(err))
			return
		}
		for _, row := range rows {
			_ = aliasesRepo.Set(bctx, row.Name, row.ID)
		}
		log.Info("alias backfill done", zap.Int("rooms", len(rows)))
	}()

	router := httpapi.Router(httpapi.Deps{
		Log:     log,
		JWT:     jwtIssuer,
		Auth:    httpapi.NewAuthHandlers(authUC, usersUC),
		Rooms:   httpapi.NewRoomHandlers(roomsUC),
		Users:   httpapi.NewUserHandlers(usersUC),
		Recs:    httpapi.NewRecordingHandlers(recsUC),
		System:  httpapi.NewSystemHandlers(auditUC),
		Layouts: httpapi.NewLayoutHandlers(layoutsUC),
	})

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())

	return &App{
		cfg:    cfg,
		log:    log,
		pool:   pool,
		rdb:    rdb,
		router: router,
		metric: mux,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	defer a.pool.Close()
	defer a.rdb.Close()

	httpSrv := &http.Server{Addr: a.cfg.HTTPAddr, Handler: a.router}
	metricSrv := &http.Server{Addr: a.cfg.MetricsAddr, Handler: a.metric}

	errCh := make(chan error, 2)
	go func() {
		a.log.Info("http listening", zap.String("addr", a.cfg.HTTPAddr))
		errCh <- httpSrv.ListenAndServe()
	}()
	go func() {
		a.log.Info("metrics listening", zap.String("addr", a.cfg.MetricsAddr))
		errCh <- metricSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		_ = httpSrv.Close()
		_ = metricSrv.Close()
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(rootCtx(), defaultShutdown)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
		_ = metricSrv.Shutdown(shutdown)
		return nil
	}
}

func (a *App) Config() *config.Config { return a.cfg }
func (a *App) Logger() *zap.Logger    { return a.log }

func dieIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
