package prepare

import (
	"context"
	"fmt"
	"time"

	"git.ozon.dev/dlukashov/sku-limiter/internal/config"
	"github.com/go-redis/redis/v7"
)

// DBSetter *redis.Client inject interface
type DBSetter interface {
	SetDB(db *redis.Client)
}

// Database inject redis to impl
func Database(ctx context.Context, impl DBSetter) error {

	dsn := config.GetValue(ctx, config.DbDsn).String()
	dbNumber := config.GetValue(ctx, config.DbNumber).Int()
	generalTimeout := time.Duration(config.GetValue(ctx, config.DbGeneralTimeout).Int()) * time.Second
	dialTimeout := time.Duration(config.GetValue(ctx, config.DbDialTimeout).Int()) * time.Second

	db := redis.NewClient(&redis.Options{
		Addr:         dsn,
		DB:           dbNumber,
		DialTimeout:  dialTimeout,
		ReadTimeout:  generalTimeout,
		WriteTimeout: generalTimeout,
		PoolTimeout:  generalTimeout,
	})

	if _, err := db.Ping().Result(); err != nil {
		return fmt.Errorf("db ping error: %w", err)
	}
	impl.SetDB(db)
	return nil
}
