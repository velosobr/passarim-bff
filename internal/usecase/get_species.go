// Caso de uso da tela de detalhe: buscar uma espécie pelo id.
package usecase

import (
	"context"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// GetSpecies valida o id e busca a espécie no catalog dentro do orçamento de
// tempo. Catalog é a porta (já com cache, breaker e retry por trás) e Budget
// é o tempo máximo da requisição inteira (REQUEST_BUDGET).
type GetSpecies struct {
	Catalog CatalogReader
	Budget  time.Duration
}

// Run devolve a espécie ou um erro classificado. Um id inválido é recusado
// antes de qualquer chamada ao catalog.
//
// context.WithTimeout cria um contexto filho que expira depois de Budget.
// Se o contexto recebido já tinha um prazo menor, vale o menor: um prazo
// filho nunca estende o do pai. O cancel liberado pelo defer solta os
// recursos do timer assim que Run termina.
func (u GetSpecies) Run(ctx context.Context, id string) (domain.Species, error) {
	if err := domain.ValidateSpeciesID(id); err != nil {
		return domain.Species{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, u.Budget)
	defer cancel()
	return u.Catalog.GetSpecies(ctx, id)
}
