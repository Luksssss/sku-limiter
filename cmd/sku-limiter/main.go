package main

import (
	context "context"

	sku_limiter "git.ozon.dev/dlukashov/sku-limiter/internal/app/sku-limiter"
	_ "git.ozon.dev/dlukashov/sku-limiter/internal/config"
	"git.ozon.dev/dlukashov/sku-limiter/internal/pkg/healthcheck"
	"git.ozon.dev/dlukashov/sku-limiter/internal/pkg/prepare"
	hc "gitlab.ozon.ru/platform/healthcheck-go"
	scratch "gitlab.ozon.ru/platform/scratch"
	_ "gitlab.ozon.ru/platform/scratch/app/pflag"
	logger "gitlab.ozon.ru/platform/tracer-go/logger"
)

func main() {
	ctx := context.Background()
	app, err := scratch.New(
		scratch.WithServiceDescriptionInterceptor(sku_limiter.Debug),
		scratch.WithUnaryInterceptor(sku_limiter.Debug),
	)
	if err != nil {
		logger.Fatalf(ctx, "can't create app: %w", err)
	}

	impl := sku_limiter.NewSKULimiter()

	healthchecks(app.Healthcheck(), impl)
	err = preparation(ctx, impl)
	if err != nil {
		logger.Fatalf(ctx, "db prepare panic: %w", err)
	}

	if err := app.Run(impl); err != nil {
		logger.Fatalf(ctx, "can't run app: %w", err)
	}

	defer impl.GetDB().Close()
}

func healthchecks(handler hc.Handler, impl *sku_limiter.Implementation) {
	healthcheck.Database(handler, impl)
}

func preparation(ctx context.Context, impl *sku_limiter.Implementation) error {

	if err := prepare.Database(ctx, impl); err != nil {
		return err
	}
	return nil
}
