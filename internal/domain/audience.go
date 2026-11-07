// A audiência de uma consulta: a jornada adulta ou a Kids (frente B). Ela
// chega na URL (?audience=adult|kids) e decide se a lista e os filtros
// trazem todas as aves ou só as que têm conteúdo Kids.

package domain

// Audience é para quem é a resposta. O valor é o mesmo da URL.
type Audience string

// As duas audiências. A adulta é o padrão: um app antigo, que não manda o
// parâmetro, continua recebendo todas as aves.
const (
	AudienceAdult Audience = "adult"
	AudienceKids  Audience = "kids"
)

// ParseAudience aceita só os valores exatos. Vazio vale a adulta.
func ParseAudience(s string) (Audience, bool) {
	switch Audience(s) {
	case "", AudienceAdult:
		return AudienceAdult, true
	case AudienceKids:
		return AudienceKids, true
	}
	return "", false
}
