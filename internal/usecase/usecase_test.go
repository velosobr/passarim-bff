// Testes dos casos de uso, com um catalog falso no lugar do real. Eles
// conferem três coisas: a validação barra a entrada ruim antes do catalog, a
// consulta chega normalizada e o orçamento de tempo está no contexto.
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
// Como a porta CatalogReader é uma interface, esta struct de teste a
// satisfaz só por ter os três métodos. Ela não busca nada: conta as
// chamadas, guarda os parâmetros e o prazo do contexto e devolve err.
type fakeCatalog struct {
	calls       int
	lastQuery   domain.ListQuery
	lastID      string
	deadline    time.Time
	hasDeadline bool
	err         error
}

// note registra uma chamada e o prazo do contexto recebido. ctx.Deadline()
// devolve o horário de expiração e se existe um.
func (f *fakeCatalog) note(ctx context.Context) {
	f.calls++
	f.deadline, f.hasDeadline = ctx.Deadline()
}

// Os três métodos abaixo registram a chamada e devolvem respostas fixas.
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

// limit=999 é inválido: o caso de uso devolve o erro de validação e o
// catalog não é chamado nenhuma vez.
func TestListSpecies_ValidatesBeforeCalling(t *testing.T) {
	f := &fakeCatalog{}
	uc := usecase.ListSpecies{Catalog: f, Budget: time.Second}
	_, err := uc.Run(context.Background(), domain.RawListQuery{Limit: "999"})
	if domain.KindOf(err) != domain.KindInvalidArgument || f.calls != 0 {
		t.Fatalf("validação deveria barrar antes do catalog: err=%v calls=%d", err, f.calls)
	}
}

// "sp" chega ao catalog como "SP", e o contexto da chamada expira entre 2 e
// 3 segundos a partir de agora, ou seja, o orçamento de 3 s foi aplicado.
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

// Um id como "../etc/passwd" (tentativa clássica de atravessar pastas) é
// recusado antes do catalog; um id válido chega com prazo no contexto.
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

// O erro do catalog atravessa o caso de uso sem perder o Kind.
func TestListFilters_AppliesBudgetAndPropagatesErrors(t *testing.T) {
	f := &fakeCatalog{err: domain.NewError(domain.KindUnavailable, errors.New("x"))}
	uc := usecase.ListFilters{Catalog: f, Budget: time.Second}
	if _, err := uc.Run(context.Background()); domain.KindOf(err) != domain.KindUnavailable || !f.hasDeadline {
		t.Fatalf("o erro do catalog deve passar intacto e com orçamento: %v", err)
	}
}

// Se quem chamou já tinha um prazo de 500 ms, o orçamento de 3 s não o
// estende: o catalog recebe o prazo menor.
func TestBudgetDoesNotExtendAnEarlierDeadline(t *testing.T) {
	f := &fakeCatalog{}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _ = usecase.GetSpecies{Catalog: f, Budget: 3 * time.Second}.Run(ctx, "a")
	if time.Until(f.deadline) > 600*time.Millisecond {
		t.Fatal("o orçamento nunca pode ESTENDER o prazo que o chamador já tinha")
	}
}
