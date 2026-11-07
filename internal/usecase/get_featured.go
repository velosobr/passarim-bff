// Caso de uso da "Ave do dia" (frente B): calcular a data de hoje em São
// Paulo e pedir ao catalog a ave dessa data.

package usecase

import (
	"context"
	"time"
	_ "time/tzdata" // embute o banco de fusos: a imagem distroless do BFF não tem o do sistema

	"github.com/velosobr/passarim-bff/internal/domain"
)

// saoPaulo é o fuso em que o "dia" da Ave do dia é contado. time/tzdata,
// importado acima, garante que LoadLocation ache o fuso em qualquer imagem.
var saoPaulo = mustLoadLocation("America/Sao_Paulo")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err) // com time/tzdata embutido, só acontece se o nome estiver errado
	}
	return loc
}

// FeaturedOutput é a ave do dia com a data dela e o max-age da resposta:
// os segundos que faltam para a próxima meia-noite de São Paulo.
type FeaturedOutput struct {
	Date     string
	Featured domain.Featured
	MaxAge   int
}

// GetFeatured busca a ave do dia dentro do orçamento de tempo. Now é o
// relógio, injetado para os testes pararem o tempo onde quiserem (em
// produção, time.Now).
type GetFeatured struct {
	Catalog CatalogReader
	Budget  time.Duration
	Now     func() time.Time
}

// Run calcula a data pelo relógio do BFF (não há parâmetro do app: a ave é
// a mesma para todos) e pede a ave ao catalog. A data entra na chave do
// cache, então à meia-noite a chave muda e a ave de ontem nunca sai.
func (u GetFeatured) Run(ctx context.Context) (FeaturedOutput, error) {
	date, maxAge := domain.FeaturedDay(u.Now(), saoPaulo)
	ctx, cancel := context.WithTimeout(ctx, u.Budget)
	defer cancel()
	f, err := u.Catalog.GetFeatured(ctx, date)
	if err != nil {
		return FeaturedOutput{}, err
	}
	return FeaturedOutput{Date: date, Featured: f, MaxAge: maxAge}, nil
}
