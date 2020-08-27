// Code generated by protoc-gen-goclay, but your can (must) modify it.
// source: sku-limiter.proto

package skulimiter

import (
	"context"

	desc "git.ozon.dev/dlukashov/sku-limiter/pkg/sku-limiter"
)

// ReturnOrder отмены и возвраты ранее купленных товаров, которые должны "плюсовать" расход лимитов
func (i *Implementation) ReturnOrder(ctx context.Context, req *desc.RORequest) (*desc.ROResponse, error) {
	err := i.returnOrderRs(ctx, req.UserId, req.OrderId, &req.Content)
	return &desc.ROResponse{Status: "OK"}, err
}
