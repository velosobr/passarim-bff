// Package media monta a URL PÚBLICA de uma mídia a partir da chave guardada
// no catalog. Trocar de CDN (SeaweedFS local → Cloudflare R2) é só mudar
// MEDIA_BASE_URL: nada no banco muda.
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

type Builder struct {
	base *url.URL
	log  *slog.Logger
}

func New(base string, log *slog.Logger) (*Builder, error) {
	u, err := url.Parse(base)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, errors.New("MEDIA_BASE_URL: esperava uma URL absoluta")
	}
	return &Builder{base: u, log: log}, nil
}

// Chaves reais: species/<id>/photo-<idNaFonte>-<variante>.webp e species/<id>/audio-<idNaFonte>.aac.
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

func hasDotSegment(key string) bool {
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." || seg == "." {
			return true
		}
	}
	return false
}
