// Testes da data da "Ave do dia": a virada à meia-noite de São Paulo e o
// max-age até ela.

package domain_test

import (
	"testing"
	"time"
	_ "time/tzdata" // o banco de fusos embutido, para o teste rodar em qualquer máquina

	"github.com/velosobr/passarim-bff/internal/domain"
)

func TestFeaturedDay_MidnightInSaoPaulo(t *testing.T) {
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		now     time.Time
		date    string
		seconds int
	}{
		{time.Date(2026, 11, 5, 23, 59, 59, 0, sp), "2026-11-05", 1},
		{time.Date(2026, 11, 5, 23, 59, 59, 900_000_000, sp), "2026-11-05", 1}, // menos de 1 s: o mínimo é 1
		{time.Date(2026, 11, 6, 0, 0, 0, 0, sp), "2026-11-06", 86400},
		{time.Date(2026, 11, 6, 2, 30, 0, 0, time.UTC), "2026-11-05", 1800}, // 23h30 em São Paulo
		{time.Date(2026, 12, 31, 12, 0, 0, 0, sp), "2026-12-31", 43200},     // virada de ano
	}
	for _, c := range cases {
		date, seconds := domain.FeaturedDay(c.now, sp)
		if date != c.date || seconds != c.seconds {
			t.Errorf("%v: %s %d, esperava %s %d", c.now, date, seconds, c.date, c.seconds)
		}
	}
}
