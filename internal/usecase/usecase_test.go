package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

// fakeCatalog guarda o que recebeu para o teste conferir.
type fakeCatalog struct {
	calls       int
	lastQuery   domain.ListQuery
	lastID      string
	deadline    time.Time
	hasDeadline bool
	err         error
}

func (f *fakeCatalog) note(ctx context.Context) {
	f.calls++
	f.deadline, f.hasDeadline = ctx.Deadline()
}

func (f *fakeCatalog) ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error) {
	f.note(ctx)
	f.lastQuery = q
	return domain.SpeciesPage{NextCursor: "n"}, f.err
}

func (f *fakeCatalog) GetSpecies(ctx context.Context, id string) (domain.Species, error) {
	f.note(ctx)
	f.lastID = id
	return domain.Species{ID: id}, f.err
}

func (f *fakeCatalog) ListFilters(ctx context.Context) (domain.Filters, error) {
	f.note(ctx)
	return domain.Filters{}, f.err
}

func TestListSpecies_ValidatesBeforeCalling(t *testing.T) {
	f := &fakeCatalog{}
	uc := usecase.ListSpecies{Catalog: f, Budget: time.Second}
	_, err := uc.Run(context.Background(), domain.RawListQuery{Limit: "999"})
	if domain.KindOf(err) != domain.KindInvalidArgument || f.calls != 0 {
		t.Fatalf("validação deveria barrar antes do catalog: err=%v calls=%d", err, f.calls)
	}
}

func TestListSpecies_PassesNormalizedQueryAndAppliesBudget(t *testing.T) {
	f := &fakeCatalog{}
	uc := usecase.ListSpecies{Catalog: f, Budget: 3 * time.Second}
	page, err := uc.Run(context.Background(), domain.RawListQuery{State: "sp", Limit: "10"})
	if err != nil || page.NextCursor != "n" {
		t.Fatalf("got %+v %v", page, err)
	}
	if f.lastQuery.State != "SP" || f.lastQuery.Limit != 10 {
		t.Fatalf("a consulta deveria chegar normalizada: %+v", f.lastQuery)
	}
	if !f.hasDeadline || time.Until(f.deadline) > 3*time.Second || time.Until(f.deadline) < 2*time.Second {
		t.Fatalf("o orçamento de 3s deveria estar no contexto: %v %v", f.hasDeadline, time.Until(f.deadline))
	}
}

func TestGetSpecies_ValidatesIDAndAppliesBudget(t *testing.T) {
	f := &fakeCatalog{}
	uc := usecase.GetSpecies{Catalog: f, Budget: time.Second}
	if _, err := uc.Run(context.Background(), "../etc/passwd"); domain.KindOf(err) != domain.KindInvalidArgument || f.calls != 0 {
		t.Fatalf("id inválido não pode chegar ao catalog: %v calls=%d", err, f.calls)
	}
	s, err := uc.Run(context.Background(), "turdus-rufiventris")
	if err != nil || s.ID != "turdus-rufiventris" || !f.hasDeadline {
		t.Fatalf("got %+v %v deadline=%v", s, err, f.hasDeadline)
	}
}

func TestListFilters_AppliesBudgetAndPropagatesErrors(t *testing.T) {
	f := &fakeCatalog{err: domain.NewError(domain.KindUnavailable, errors.New("x"))}
	uc := usecase.ListFilters{Catalog: f, Budget: time.Second}
	if _, err := uc.Run(context.Background()); domain.KindOf(err) != domain.KindUnavailable || !f.hasDeadline {
		t.Fatalf("o erro do catalog deve passar intacto e com orçamento: %v", err)
	}
}

func TestBudgetDoesNotExtendAnEarlierDeadline(t *testing.T) {
	f := &fakeCatalog{}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _ = usecase.GetSpecies{Catalog: f, Budget: 3 * time.Second}.Run(ctx, "a")
	if time.Until(f.deadline) > 600*time.Millisecond {
		t.Fatal("o orçamento nunca pode ESTENDER o prazo que o chamador já tinha")
	}
}
