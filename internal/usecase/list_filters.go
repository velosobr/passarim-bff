package usecase

import (
	"context"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
)

type ListFilters struct {
	Catalog CatalogReader
	Budget  time.Duration
}

func (u ListFilters) Run(ctx context.Context) (domain.Filters, error) {
	ctx, cancel := context.WithTimeout(ctx, u.Budget)
	defer cancel()
	return u.Catalog.ListFilters(ctx)
}
