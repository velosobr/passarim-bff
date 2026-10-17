// Testes dos valores válidos de bioma e de UF.

package domain_test

import (
	"testing"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// Os seis códigos oficiais passam e voltam iguais; qualquer variação
// (maiúscula, vazio, bioma inventado) é recusada. Se um código mudasse aqui
// sem mudar no catalog, o filtro de bioma deixaria de funcionar.
func TestParseBiome(t *testing.T) {
	for _, code := range []string{"amazonia", "mata_atlantica", "cerrado", "caatinga", "pantanal", "pampa"} {
		if b, ok := domain.ParseBiome(code); !ok || string(b) != code {
			t.Errorf("ParseBiome(%q) = %q, %v", code, b, ok)
		}
	}
	for _, bad := range []string{"", "CERRADO", "Cerrado", "deserto"} {
		if _, ok := domain.ParseBiome(bad); ok {
			t.Errorf("ParseBiome(%q) deveria falhar (códigos são exatos)", bad)
		}
	}
}

// "sp" vira "SP", uma sigla inexistente é recusada e a lista completa tem as
// 27 UFs (26 estados e o Distrito Federal).
func TestNormalizeUF(t *testing.T) {
	if uf, ok := domain.NormalizeUF("sp"); !ok || uf != "SP" {
		t.Errorf("sp → %q %v", uf, ok)
	}
	if _, ok := domain.NormalizeUF("XX"); ok {
		t.Error("XX não é UF")
	}
	if len(domain.AllUFs()) != 27 {
		t.Errorf("deveria haver 27 UFs, há %d", len(domain.AllUFs()))
	}
}
