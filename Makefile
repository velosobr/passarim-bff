# Atalhos do dia a dia. Uso: make test | make lint | make e2e | make run
.PHONY: test lint e2e run

# Testes unitários, de contrato e de integração com Redis (testcontainers: precisa de Docker).
test:
	go test -race ./...

lint:
	golangci-lint run

# Teste ponta a ponta com o catalog real. Só local: constrói o catalog de ../passarim-catalog.
e2e:
	PASSARIM_CATALOG_DIR=$(abspath ../passarim-catalog) go test -race -tags e2e ./e2e/...

# Roda o BFF local apontando para o catalog e o Redis do docker compose (portas do host).
run:
	CATALOG_ADDR=localhost:50051 REDIS_URL=redis://localhost:6379 MEDIA_BASE_URL=http://localhost:8888/buckets/passarim-media go run ./cmd/bff
