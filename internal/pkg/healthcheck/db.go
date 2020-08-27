package healthcheck

import (
	"fmt"

	"github.com/go-redis/redis/v7"
	hc "gitlab.ozon.ru/platform/healthcheck-go"
)

// DBGetter retrieve interface for *redis.Client
type DBGetter interface {
	GetDB() *redis.Client
}

// Database init database healthcheck
func Database(handler hc.Handler, impl DBGetter) {
	handler.AddReadinessCheck("db", func() error {
		db := impl.GetDB()
		if db == nil {
			return fmt.Errorf("db is nil")
		}

		if _, err := db.Ping().Result(); err != nil {
			return fmt.Errorf("db ping error: %w", err)
		}

		return nil
	})
}
