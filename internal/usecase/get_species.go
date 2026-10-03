package usecase

import (
	"context"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
)

type GetSpecies struct {
	Catalog CatalogReader
	Budget  time.Duration
}

func (u GetSpecies) Run(ctx context.Context, id string) (domain.Species, error) {
	if err := domain.ValidateSpeciesID(id); err != nil {
		return domain.Species{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, u.Budget)
	defer cancel()
	return u.Catalog.GetSpecies(ctx, id)
}
