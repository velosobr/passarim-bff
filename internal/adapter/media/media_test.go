package media_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/velosobr/passarim-bff/internal/adapter/media"
)

func build(t *testing.T, base string) *media.Builder {
	t.Helper()
	b, err := media.New(base, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMediaURL_JoinsBaseAndKey(t *testing.T) {
	const key = "species/turdus-rufiventris/photo-12345-thumb.webp"
	want := "http://localhost:8888/buckets/passarim-media/" + key
	for _, base := range []string{"http://localhost:8888/buckets/passarim-media", "http://localhost:8888/buckets/passarim-media/"} {
		if u := build(t, base).URL(context.Background(), key); u == nil || *u != want {
			t.Errorf("base %q → %v, esperado %s", base, u, want)
		}
	}
}

func TestMediaURL_EmptyKeyIsNil(t *testing.T) {
	if u := build(t, "http://x/b").URL(context.Background(), ""); u != nil {
		t.Fatalf("chave vazia deveria dar nil, veio %q", *u)
	}
}

// Review Focus #7: a chave vem do catalog, mas não confiamos cegamente.
func TestMediaURL_RejectsUnsafeKeys(t *testing.T) {
	b := build(t, "http://x/bucket")
	for _, key := range []string{"../outro-bucket/x.webp", "species/../../x", "/abs/x.webp", "species//x.webp", "species/a b.webp", "species/x.webp?x=1", "species/é.webp", "a/./b"} {
		if u := b.URL(context.Background(), key); u != nil {
			t.Errorf("chave %q deveria ser recusada, gerou %q", key, *u)
		}
	}
}

func TestNew_RejectsRelativeBase(t *testing.T) {
	if _, err := media.New("/relativo", slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("base relativa deveria falhar")
	}
}
