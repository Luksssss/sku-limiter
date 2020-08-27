package skulimiter

import (
	"context"

	"gitlab.ozon.ru/platform/tracer-go/logger"
	"google.golang.org/grpc"
)

// Debug смотрим запрос-ответ
func Debug(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	logger.Debugf(ctx, "request: %#v", req)
	resp, err = handler(ctx, req)
	logger.Debugf(ctx, "response: %#v", resp)
	return
}
