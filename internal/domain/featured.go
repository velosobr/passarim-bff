// A data da "Ave do dia" (frente B). A ave troca à meia-noite de São Paulo,
// para todo mundo ao mesmo tempo: a data sai do relógio do BFF nesse fuso, e
// o app guarda a resposta só até essa meia-noite.

package domain

import "time"

// FeaturedDay devolve a data da "Ave do dia" no instante now, no formato
// AAAA-MM-DD do fuso loc, e quantos segundos faltam para a próxima
// meia-noite desse fuso: é o max-age da resposta. O mínimo é 1, porque
// "max-age=0" faria o app pedir de novo sem parar no último segundo do dia.
//
// time.Date com o dia + 1 resolve sozinho a virada de mês e de ano (32 de
// janeiro vira 1º de fevereiro).
func FeaturedDay(now time.Time, loc *time.Location) (date string, secondsLeft int) {
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)
	return local.Format(time.DateOnly), max(int(midnight.Sub(local)/time.Second), 1)
}
