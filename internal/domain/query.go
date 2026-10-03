package domain

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultLimit = 20
	MaxLimit     = 50
	maxQueryLen  = 100
	maxCursorLen = 512
	maxIDLen     = 80
)

// FieldError aponta UM parâmetro inválido (vira errors[] no problem+json).
type FieldError struct {
	Field, Reason string
}

// ValidationError reúne TODOS os problemas de uma requisição de uma vez.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string { return "parâmetros inválidos" }

// RawListQuery são as strings cruas da URL (vazio = ausente). Repeated lista
// parâmetros que vieram mais de uma vez (o handler detecta; aqui só reportamos).
type RawListQuery struct {
	Q, Biome, State, Cursor, Limit string
	Repeated                       []string
}

// ListQuery é a consulta JÁ validada e normalizada. A chave de cache é calculada em cima dela.
type ListQuery struct {
	Q      string
	Biome  Biome
	State  string
	Cursor string
	Limit  int
}

// NewListQuery valida e normaliza. Devolve *ValidationError com todos os campos ruins.
func NewListQuery(raw RawListQuery) (ListQuery, error) {
	var bad []FieldError
	q := ListQuery{Limit: DefaultLimit}

	for _, f := range raw.Repeated {
		bad = append(bad, FieldError{f, "não pode ser repetido"})
	}

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
