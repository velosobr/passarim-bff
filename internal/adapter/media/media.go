// Package media monta a URL PÚBLICA de uma mídia a partir da chave guardada
// no catalog. Trocar de CDN (SeaweedFS local → Cloudflare R2) é só mudar
// MEDIA_BASE_URL: nada no banco muda.
//
// O catalog guarda só a "chave" de cada arquivo no object storage (o
// caminho dentro do bucket, como "species/x/photo-1-thumb.webp"), e não a
// URL completa (ADR-0006). O BFF junta a base pública com a chave na hora de
// responder ao app.
package media

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
)

// Builder monta as URLs. base é a URL pública já interpretada, e log avisa
// quando uma chave suspeita é descartada.
type Builder struct {
	base *url.URL
	log  *slog.Logger
}

// New interpreta a base (MEDIA_BASE_URL). Ela precisa ser absoluta, com
// esquema e host; uma base relativa geraria URLs que o app não consegue abrir.
func New(base string, log *slog.Logger) (*Builder, error) {
	u, err := url.Parse(base)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, errors.New("MEDIA_BASE_URL: esperava uma URL absoluta")
	}
	return &Builder{base: u, log: log}, nil
}

// Chaves reais: species/<id>/photo-<idNaFonte>-<variante>.webp e species/<id>/audio-<idNaFonte>.aac.
//
// safeKey é uma expressão regular: só letras sem acento, números, ponto,
// sublinhado, barra e hífen. MustCompile interpreta a expressão quando o
// programa sobe e encerra com pânico se ela estiver errada (aceitável para
// uma constante do código, que o primeiro teste já pega).
var safeKey = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// URL devolve nil para chave vazia (espécie ainda sem foto) ou insegura.
// Defesa em profundidade: url.JoinPath "limpa" ".." e uma chave maliciosa
// poderia sair do bucket, então recusamos antes.
func (b *Builder) URL(ctx context.Context, key string) *string {
	if key == "" {
		return nil
	}
	if !safeKey.MatchString(key) || strings.HasPrefix(key, "/") || strings.Contains(key, "//") || hasDotSegment(key) {
		b.log.WarnContext(ctx, "chave de mídia insegura descartada", "key", key, "request_id", reqmeta.RequestID(ctx))
		return nil
	}
	s, err := url.JoinPath(b.base.String(), key)
	if err != nil {
		return nil
	}
	return &s
}

// hasDotSegment diz se algum trecho entre barras é "." ou "..", os
// segmentos que mudam de pasta num caminho.
func hasDotSegment(key string) bool {
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." || seg == "." {
			return true
		}
	}
	return false
}
