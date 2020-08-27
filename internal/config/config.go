// Code generated by "scratch generate config". DO NOT EDIT.

package config

import (
	"context"

	realtimeconfig "gitlab.ozon.ru/platform/realtime-config-go"
	"gitlab.ozon.ru/platform/scratch/app"
	"gitlab.ozon.ru/platform/scratch/config"
	"gitlab.ozon.ru/platform/tracer-go/logger"
)

type configKey string

// Strictly-typed config keys.
// Keys are parsed from values.yaml "realtimeconfig" section and values_production.yaml "deploy.env" section.
const (
	DbDialTimeout    = configKey("db_dial_timeout")
	DbDsn            = configKey("db_dsn")
	DbGeneralTimeout = configKey("db_general_timeout")
	DbNumber         = configKey("db_number")
	// Log level enum
	LogLevel = configKey("log_level")
)

const (
	// AppName is parsed from .gitlab-ci.yml SERVICE_NAME variable
	AppName = "sku-limiter"

	// AppNS is parsed from .gitlab-ci.yml K8S_NAMESPACE variable
	// or if K8S_NAMESPACE has ENV $variable it is parsed from project module name 'gitlab.ozon.ru/<ns>/<project>'
	AppNS = "dlukashov"
)

func init() {
	app.SetName(AppName)
	app.SetNamespace(AppNS)
}

// GetValue returns realtimeconfig.Value for a given strictly-typed configKey.
//
// If you want to mock the client, use scratch/config.SetClient() function. Restriction - you won't be able to use T.Parallel().
// Use NewClient() instead, to be more flexible in mocking.
func GetValue(ctx context.Context, key configKey) realtimeconfig.Value {
	return getValue(config.Client(), ctx, key)
}

// Client is a simple facade for realtimeconfig.Client.
//
// Use it directly for more flexible mocking of realtimeconfig.Client.
// See comments for config.GetValue() func.
type Client struct {
	client realtimeconfig.Client
}

// NewClient returns new Client instance.
func NewClient(c realtimeconfig.Client) *Client {
	return &Client{client: c}
}

// GetValue returns realtimeconfig.Value for a given strictly-typed configKey.
func (c *Client) GetValue(ctx context.Context, key configKey) realtimeconfig.Value {
	return getValue(c.client, ctx, key)
}

func getValue(client realtimeconfig.Client, ctx context.Context, key configKey) realtimeconfig.Value {
	v, err := client.Value(ctx, string(key))
	if err == nil {
		return v
	}

	switch err {
	case realtimeconfig.ErrVariableNotFound:
		logger.Warnf(ctx, "%#q config variable not found!", key)
	default:
		logger.Errorf(ctx, "unexpected get %#q config error: %s", key, err)
	}
	return realtimeconfig.NewNilValue(err)
}