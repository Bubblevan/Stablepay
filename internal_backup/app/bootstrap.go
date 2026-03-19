package app

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/redis/go-redis/v9"
	"stablepay/api-gateway/internal/application"
	"stablepay/api-gateway/internal/infrastructure/auth"
	"stablepay/api-gateway/internal/infrastructure/clients"
	"stablepay/api-gateway/internal/infrastructure/config"
	"stablepay/api-gateway/internal/infrastructure/observability"
	"stablepay/api-gateway/internal/infrastructure/ratelimit"
	"stablepay/api-gateway/internal/interfaces/http"
	"stablepay/api-gateway/internal/interfaces/http/middleware"
)

type Instance struct {
	Engine     *server.Hertz
	ReadyState *middleware.ReadyState
}

func New(cfg *config.AppConfig, logger *observability.Logger) (*Instance, error) {
	h := server.Default(
		server.WithHostPorts(cfg.Server.Address),
		server.WithReadTimeout(time.Duration(cfg.Server.ReadTimeoutMS)*time.Millisecond),
		server.WithWriteTimeout(time.Duration(cfg.Server.WriteTimeoutMS)*time.Millisecond),
	)

	var limiter ratelimit.Limiter
	var nonceStore auth.NonceStore

	if cfg.Redis.Enabled {
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err := rdb.Ping(context.Background()).Err(); err == nil {
			limiter = ratelimit.NewRedisLimiter(rdb, "stablepay:rl:")
			nonceStore = auth.NewRedisNonceStore(rdb, "stablepay:nonce:")
		} else {
			logger.Warn("redis disabled due to ping failure", map[string]interface{}{"error": err.Error()})
		}
	}
	if limiter == nil {
		limiter = ratelimit.NewMemoryLimiter()
	}
	if nonceStore == nil {
		nonceStore = auth.NewMemoryNonceStore()
	}

	didClient := clients.NewMockDIDClient()
	paymentClient := clients.NewMockPaymentClient()
	verificationClient := clients.NewMockVerificationClient()
	queryClient := clients.NewMockQueryClient()

	appService := application.NewService(didClient, paymentClient, verificationClient, queryClient)
	handler := http.NewHandler(appService)
	readyState := middleware.NewReadyState()

	h.Use(
		middleware.Recovery(),
		middleware.RequestMeta(),
		middleware.ReadinessGuard(readyState),
		middleware.AuthExtract(),
		middleware.AccessLog(logger),
	)
	http.RegisterRoutes(
		h,
		handler,
		cfg.Routes,
		readyState,
		middleware.AuthVerify(cfg.Security, didClient),
		middleware.ReplayProtection(cfg.Security, nonceStore),
		middleware.RateLimit(limiter),
	)

	return &Instance{Engine: h, ReadyState: readyState}, nil
}

func (i *Instance) Run() error {
	if i.Engine == nil {
		return fmt.Errorf("engine not initialized")
	}
	return i.Engine.Run()
}
