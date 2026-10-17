// Validação dos parâmetros que chegam pela URL. Tudo o que vem do cliente é
// tratado como suspeito: tamanho máximo, caracteres permitidos e valores
// conhecidos são conferidos aqui, antes de qualquer chamada ao catalog ou ao
// cache (OWASP API: validar toda entrada na borda).

package domain

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limites da API. Os exportados (com letra maiúscula) aparecem no contrato
// REST: DefaultLimit é o tamanho de página quando o cliente não pede um, e
// MaxLimit é o maior permitido. Os não exportados (com minúscula) só existem
// dentro deste pacote: o tamanho máximo da busca, do cursor e do id.
const (
	DefaultLimit = 20
	MaxLimit     = 50
	maxQueryLen  = 100
	maxCursorLen = 512
	maxIDLen     = 80
)

// FieldError aponta UM parâmetro inválido (vira errors[] no problem+json).
//
// problem+json (RFC 9457) é um formato padrão de resposta de erro em JSON. O
// BFF o usa em todas as respostas de erro; a lista "errors" diz ao app qual
// parâmetro estava errado e por quê.
type FieldError struct {
	Field, Reason string
}

// ValidationError reúne TODOS os problemas de uma requisição de uma vez.
//
// Juntar tudo poupa idas e voltas: quem chama a API corrige todos os
// parâmetros numa tentativa só, em vez de descobrir um erro por vez.
type ValidationError struct {
	Fields []FieldError
}

// Error faz o *ValidationError ser um erro. A mensagem é genérica porque os
// detalhes vão no campo Fields, que o adapter HTTP transforma em JSON.
func (e *ValidationError) Error() string { return "parâmetros inválidos" }

// RawListQuery são as strings cruas da URL (vazio = ausente). Repeated lista
// parâmetros que vieram mais de uma vez (o handler detecta; aqui só reportamos).
//
// Um parâmetro repetido (?biome=cerrado&biome=pampa) é recusado porque a
// API aceita um valor por filtro; aceitar só o primeiro esconderia o engano
// de quem chamou.
type RawListQuery struct {
	Q, Biome, State, Cursor, Limit string
	Repeated                       []string
}

// ListQuery é a consulta JÁ validada e normalizada. A chave de cache é calculada em cima dela.
//
// Separar o tipo "cru" do tipo "validado" é uma técnica comum: quem recebe um
// ListQuery sabe que ele já passou pelas regras, sem precisar conferir de
// novo. Como ele só é criado por NewListQuery, não existe ListQuery inválido
// circulando pelo código.
type ListQuery struct {
	Q      string
	Biome  Biome
	State  string
	Cursor string
	Limit  int
}

// NewListQuery valida e normaliza. Devolve *ValidationError com todos os campos ruins.
//
// Cada parâmetro é conferido em sequência, e os problemas vão se acumulando
// em "bad". Só no fim decidimos: se houve algum problema, nada da consulta é
// usado.
func NewListQuery(raw RawListQuery) (ListQuery, error) {
	var bad []FieldError
	q := ListQuery{Limit: DefaultLimit}

	for _, f := range raw.Repeated {
		bad = append(bad, FieldError{f, "não pode ser repetido"})
	}

	// Busca: espaços nas pontas são removidos. Depois, o texto precisa ser
	// UTF-8 válido, ter no máximo 100 caracteres (contados como letras, com
	// RuneCountInString, e não como bytes: "ç" ocupa 2 bytes) e não ter
	// caracteres de controle (como quebra de linha), que poderiam sujar logs.
	if s := strings.TrimSpace(raw.Q); s != "" {
		switch {
		case !utf8.ValidString(s):
			bad = append(bad, FieldError{"q", "texto inválido"})
		case utf8.RuneCountInString(s) > maxQueryLen:
			bad = append(bad, FieldError{"q", "no máximo 100 caracteres"})
		case strings.IndexFunc(s, unicode.IsControl) >= 0:
			bad = append(bad, FieldError{"q", "não pode conter caracteres de controle"})
		default:
			q.Q = s
		}
	}
	if raw.Biome != "" {
		if b, ok := ParseBiome(raw.Biome); ok {
			q.Biome = b
		} else {
			bad = append(bad, FieldError{"biome", "bioma desconhecido"})
		}
	}
	if raw.State != "" {
		if uf, ok := NormalizeUF(raw.State); ok {
			q.State = uf
		} else {
			bad = append(bad, FieldError{"state", "UF desconhecida"})
		}
	}
	if raw.Cursor != "" {
		if validCursor(raw.Cursor) {
			q.Cursor = raw.Cursor
		} else {
			bad = append(bad, FieldError{"cursor", "cursor inválido"})
		}
	}
	if raw.Limit != "" {
		n, err := strconv.Atoi(raw.Limit)
		if err != nil || n < 1 || n > MaxLimit {
			bad = append(bad, FieldError{"limit", "deve ser um inteiro entre 1 e 50"})
		} else {
			q.Limit = n
		}
	}
	if len(bad) > 0 {
		return ListQuery{}, &ValidationError{Fields: bad}
	}
	return q, nil
}

// O cursor é opaco para nós, mas vem do cliente: só repassamos se parecer base64url curto.
//
// "Opaco" quer dizer que o BFF não entende o conteúdo: o catalog o criou na
// página anterior e é ele quem o lê. Mesmo assim, conferimos o formato
// (letras, números, "_" e "-", até 512 caracteres) para não repassar lixo
// nem textos enormes ao catalog.
func validCursor(s string) bool {
	if len(s) > maxCursorLen {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// ValidateSpeciesID aceita só ^[a-z0-9-]{1,80}$.
//
// O id de uma espécie é um "slug" como "turdus-rufiventris": minúsculas,
// números e hífen. Conferir isso antes de ir ao catalog barra ids absurdos
// sem gastar uma chamada, e evita que um id estranho vire chave de cache.
func ValidateSpeciesID(id string) error {
	if id == "" || len(id) > maxIDLen {
		return &ValidationError{Fields: []FieldError{{"id", "identificador inválido"}}}
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return &ValidationError{Fields: []FieldError{{"id", "identificador inválido"}}}
		}
	}
	return nil
}
