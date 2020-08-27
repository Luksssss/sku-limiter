include scratch.mk

testv:
	go test -v ./internal/app/sku-limiter/ -race -count=1

# run only benchmarks
bench:
	go test ./internal/app/sku-limiter/ -bench=. -run=^a -v -count=1 -benchtime 100x
