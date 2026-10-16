// Testes da validação dos parâmetros da lista e do id de espécie.

package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// fields extrai de um erro os nomes dos parâmetros que ele aponta, como um
// conjunto. t.Helper() faz uma falha aqui ser reportada na linha do teste
// que chamou fields, e não dentro dela.
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

// Sem nenhum parâmetro, a consulta é "tudo, 20 por página".
func TestNewListQuery_Defaults(t *testing.T) {
	q, err := domain.NewListQuery(domain.RawListQuery{})
	if err != nil || q.Limit != 20 || q.Q != "" || q.Biome != "" || q.State != "" || q.Cursor != "" {
		t.Fatalf("padrões errados: %+v %v", q, err)
	}
}

// Espaços nas pontas da busca somem, a UF vai para maiúsculas e os demais
// valores válidos passam como vieram.
func TestNewListQuery_Normalizes(t *testing.T) {
	q, err := domain.NewListQuery(domain.RawListQuery{Q: "  sabiá  ", State: "sp", Biome: "cerrado", Limit: "50", Cursor: "abc_-123"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Q != "sabiá" || q.State != "SP" || q.Biome != domain.BiomeCerrado || q.Limit != 50 || q.Cursor != "abc_-123" {
		t.Fatalf("normalização errada: %+v", q)
	}
}

// Com vários parâmetros errados de uma vez, o erro aponta todos eles, e não
// só o primeiro.
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

// Fora de 1..50, o limit é recusado com erro. Ele nunca é "consertado" em
// silêncio (por exemplo, 51 virando 50), para quem chama perceber o engano.
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

// As regras da busca: sem caracteres de controle, só UTF-8 válido, o limite
// de 100 contado em letras (100 "é" ocupam 200 bytes e passam) e "só
// espaços" contando como busca vazia.
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

// O id aceita minúsculas, números e hífen, com 1 a 80 caracteres. Barra,
// "..", maiúscula, sublinhado e acento são recusados: um id assim nunca é
// repassado ao catalog nem vira chave de cache.
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
