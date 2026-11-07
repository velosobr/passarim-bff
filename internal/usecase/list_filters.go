// Caso de uso da tela de filtros: listar os biomas e as UFs com espécies.

package usecase

import (
	"context"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// ListFilters busca as opções de filtro no catalog dentro do orçamento de
// tempo. O único parâmetro é a audiência (frente B), validada antes de
// qualquer chamada.
type ListFilters struct {
	Catalog CatalogReader
	Budget  time.Duration
}

// Run valida a audiência, aplica o orçamento e repassa a chamada à porta. O
// erro do catalog volta como veio, já classificado com um Kind.
func (u ListFilters) Run(ctx context.Context, raw domain.RawFiltersQuery) (domain.Filters, error) {
	q, err := domain.NewFiltersQuery(raw)
	if err != nil {
		return domain.Filters{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, u.Budget)
	defer cancel()
	return u.Catalog.ListFilters(ctx, q)
}
