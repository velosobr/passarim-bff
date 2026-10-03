package usecase

import (
	"context"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// ListSpecies valida os parâmetros (domain) ANTES de gastar uma chamada ao
// catalog e aplica o orçamento total de tempo da requisição. A cadeia de
// resiliência (cache, breaker, retry) fica atrás da porta Catalog.
type ListSpecies struct {
	Catalog CatalogReader
	Budget  time.Duration
}

func (u ListSpecies) Run(ctx context.Context, raw domain.RawListQuery) (domain.SpeciesPage, error) {
	q, err := domain.NewListQuery(raw)
	if err != nil {
		return domain.SpeciesPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, u.Budget) // nunca estende um prazo anterior, só encurta
	defer cancel()
	return u.Catalog.ListSpecies(ctx, q)
}
