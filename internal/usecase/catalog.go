// Package usecase contém os casos de uso do BFF e a PORTA que eles usam para
// falar com o catalog. Só regras e interfaces: gRPC, Redis e HTTP ficam nos adapters.
//
// Um "caso de uso" é uma ação que o sistema oferece (listar espécies, ver uma
// espécie, ver os filtros). Cada um é uma struct com um método Run: recebe a
// entrada, valida, aplica o orçamento de tempo e chama a porta.
//
// "Porta" é uma interface definida aqui, do lado de quem USA, e implementada
// do lado de fora (nos adapters). Em Go, uma interface é satisfeita
// implicitamente: qualquer tipo com estes três métodos já é um CatalogReader,
// sem declarar nada. Por isso os testes trocam o catalog real por um falso
// com uma struct de poucas linhas.
package usecase

import (
	"context"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// CatalogReader é tudo o que o BFF precisa do catalog. Quem implementa:
// o cliente gRPC e cada decorator (retry, breaker, cache), todos com a MESMA interface.
type CatalogReader interface {
	// ListSpecies devolve uma página da lista, já filtrada pela consulta.
	ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error)
	// GetSpecies devolve uma espécie completa, ou um erro KindNotFound.
	GetSpecies(ctx context.Context, id string) (domain.Species, error)
	// ListFilters devolve os biomas e as UFs que têm espécies, com as contagens.
	ListFilters(ctx context.Context) (domain.Filters, error)
}
