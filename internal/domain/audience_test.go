// Testes da audiência (frente B) e da validação de /v1/filters.

package domain_test

import (
	"errors"
	"testing"

	"github.com/velosobr/passarim-bff/internal/domain"
)

func TestParseAudience(t *testing.T) {
	cases := map[string]struct {
		want domain.Audience
		ok   bool
	}{"": {domain.AudienceAdult, true}, "adult": {domain.AudienceAdult, true}, "kids": {domain.AudienceKids, true},
		"Kids": {"", false}, "criancas": {"", false}}
	for in, c := range cases {
		if got, ok := domain.ParseAudience(in); got != c.want || ok != c.ok {
			t.Errorf("%q: %q %v, esperava %q %v", in, got, ok, c.want, c.ok)
		}
	}
}

// A lista aceita audience e primaryBiome; os valores inválidos aparecem
// junto com os outros problemas.
func TestNewListQuery_AudienceAndPrimaryBiome(t *testing.T) {
	q, err := domain.NewListQuery(domain.RawListQuery{Audience: "kids", PrimaryBiome: "pampa"})
	if err != nil || q.Audience != domain.AudienceKids || q.PrimaryBiome != domain.BiomePampa {
		t.Fatalf("got %+v %v", q, err)
	}
	if q, _ := domain.NewListQuery(domain.RawListQuery{}); q.Audience != domain.AudienceAdult {
		t.Fatalf("o padrão é adult: %+v", q)
	}
	_, err = domain.NewListQuery(domain.RawListQuery{Audience: "todos", PrimaryBiome: "lua", Limit: "0"})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) != 3 {
		t.Fatalf("esperava 3 problemas (audience, primaryBiome, limit), veio %v", err)
	}
}

func TestNewFiltersQuery(t *testing.T) {
	if q, err := domain.NewFiltersQuery(domain.RawFiltersQuery{Audience: "kids"}); err != nil || q.Audience != domain.AudienceKids {
		t.Fatalf("got %+v %v", q, err)
	}
	if q, err := domain.NewFiltersQuery(domain.RawFiltersQuery{}); err != nil || q.Audience != domain.AudienceAdult {
		t.Fatalf("o padrão é adult: %+v %v", q, err)
	}
	_, err := domain.NewFiltersQuery(domain.RawFiltersQuery{Audience: "x", Repeated: []string{"audience"}})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) != 2 {
		t.Fatalf("esperava 2 problemas, veio %v", err)
	}
}
