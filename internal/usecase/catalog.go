// Package usecase contém os casos de uso do BFF e a PORTA que eles usam para
// falar com o catalog. Só regras e interfaces: gRPC, Redis e HTTP ficam nos adapters.
package usecase

import (
	"context"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// CatalogReader é tudo o que o BFF precisa do catalog. Quem implementa:
// o cliente gRPC e cada decorator (retry, breaker, cache), todos com a MESMA interface.
type CatalogReader interface {
	ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error)
	GetSpecies(ctx context.Context, id string) (domain.Species, error)
	ListFilters(ctx context.Context) (domain.Filters, error)
}
