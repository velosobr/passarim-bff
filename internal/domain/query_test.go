package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/velosobr/passarim-bff/internal/domain"
)

func fields(t *testing.T, err error) map[string]bool {
	t.Helper()
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("esperava ValidationError, veio %v", err)
	}
	m := map[string]bool{}
	for _, f := range ve.Fields {
		m[f.Field] = true
	}
	return m
}

func TestNewListQuery_Defaults(t *testing.T) {
	q, err := domain.NewListQuery(domain.RawListQuery{})
	if err != nil || q.Limit != 20 || q.Q != "" || q.Biome != "" || q.State != "" || q.Cursor != "" {
		t.Fatalf("padrões errados: %+v %v", q, err)
	}
}

func TestNewListQuery_Normalizes(t *testing.T) {
	q, err := domain.NewListQuery(domain.RawListQuery{Q: "  sabiá  ", State: "sp", Biome: "cerrado", Limit: "50", Cursor: "abc_-123"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Q != "sabiá" || q.State != "SP" || q.Biome != domain.BiomeCerrado || q.Limit != 50 || q.Cursor != "abc_-123" {
		t.Fatalf("normalização errada: %+v", q)
	}
}

func TestNewListQuery_RejectsAndReportsAllFields(t *testing.T) {
	_, err := domain.NewListQuery(domain.RawListQuery{
		Q: strings.Repeat("a", 101), Biome: "deserto", State: "XX", Cursor: "tem espaço", Limit: "0", Repeated: []string{"state"},
	})
	got := fields(t, err)
	for _, f := range []string{"q", "biome", "state", "cursor", "limit"} {
		if !got[f] {
			t.Errorf("campo %q deveria estar no erro: %v", f, got)
		}
	}
}

func TestNewListQuery_LimitBounds(t *testing.T) {
	for _, bad := range []string{"0", "51", "-1", "abc", "1.5"} {
		if _, err := domain.NewListQuery(domain.RawListQuery{Limit: bad}); err == nil {
			t.Errorf("limit=%q deveria ser recusado (nunca ajustado em silêncio)", bad)
		}
	}
	for _, ok := range []string{"1", "20", "50"} {
		if _, err := domain.NewListQuery(domain.RawListQuery{Limit: ok}); err != nil {
			t.Errorf("limit=%q deveria passar: %v", ok, err)
		}
	}
}

func TestNewListQuery_QueryRules(t *testing.T) {
	if _, err := domain.NewListQuery(domain.RawListQuery{Q: "a\x00b"}); err == nil {
		t.Error("caractere de controle deveria ser recusado")
	}
	if _, err := domain.NewListQuery(domain.RawListQuery{Q: "\xff\xfe"}); err == nil {
		t.Error("UTF-8 inválido deveria ser recusado")
	}
	if _, err := domain.NewListQuery(domain.RawListQuery{Q: strings.Repeat("é", 100)}); err != nil {
		t.Errorf("100 runas (200 bytes) deveriam passar: %v", err)
	}
	q, err := domain.NewListQuery(domain.RawListQuery{Q: "    "})
	if err != nil || q.Q != "" {
		t.Errorf("só espaços = sem busca: %+v %v", q, err)
	}
}

func TestValidateSpeciesID(t *testing.T) {
	for _, ok := range []string{"turdus-rufiventris", "a", strings.Repeat("a", 80)} {
		if err := domain.ValidateSpeciesID(ok); err != nil {
			t.Errorf("%q deveria ser válido: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Turdus", "a_b", "a/b", "../x", strings.Repeat("a", 81), "é"} {
		if err := domain.ValidateSpeciesID(bad); err == nil {
			t.Errorf("%q deveria ser inválido", bad)
		}
	}
}
