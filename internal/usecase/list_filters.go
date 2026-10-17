// Caso de uso da tela de filtros: listar os biomas e as UFs com espécies.

package usecase

import (
	"context"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// ListFilters busca as opções de filtro no catalog dentro do orçamento de
// tempo. Não há parâmetros de entrada, então não há o que validar.
type ListFilters struct {
	Catalog CatalogReader
	Budget  time.Duration
}

// Run aplica o orçamento e repassa a chamada à porta. O erro do catalog volta
// como veio, já classificado com um Kind.
func (u ListFilters) Run(ctx context.Context) (domain.Filters, error) {
	ctx, cancel := context.WithTimeout(ctx, u.Budget)
	defer cancel()
	return u.Catalog.ListFilters(ctx)
}
