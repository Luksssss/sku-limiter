// Code generated by protoc-gen-goclay, but your can (must) modify it.
// source: sku-limiter.proto

package skulimiter

import (
	"context"

	desc "git.ozon.dev/dlukashov/sku-limiter/pkg/sku-limiter"
)

// CreateLimitSKU задать лимиты для списка sku
func (i *Implementation) CreateLimitSKU(ctx context.Context, req *desc.CLRequest) (*desc.CLResponse, error) {
	err := i.createLimitSKURs(ctx, &req.Skus)
	return &desc.CLResponse{Status: "OK"}, err
}