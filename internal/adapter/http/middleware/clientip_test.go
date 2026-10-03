package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/velosobr/passarim-bff/internal/adapter/http/middleware"
)

func req(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func trusted(cidrs ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, c := range cidrs {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}

// Review Focus #3: sem proxy confiável, o cabeçalho é IGNORADO (qualquer cliente poderia forjá-lo).
func TestClientIP_IgnoresHeaderWhenConnectionNotTrusted(t *testing.T) {
	ip := middleware.NewIPResolver(trusted("172.30.0.10/32"), "X-Forwarded-For")
	if got := ip(req("198.51.100.7:5555", "1.2.3.4")); got != "198.51.100.7" {
		t.Fatalf("got %s", got)
	}
	if got := middleware.NewIPResolver(nil, "X-Forwarded-For")(req("198.51.100.7:5555", "1.2.3.4")); got != "198.51.100.7" {
		t.Fatalf("sem lista de proxies, ninguém é confiável: %s", got)
	}
}

func TestClientIP_TrustedProxyUsesRightmostNonTrusted(t *testing.T) {
	ip := middleware.NewIPResolver(trusted("172.30.0.10/32", "10.0.0.0/8"), "X-Forwarded-For")
	cases := map[string]struct {
		xff  []string
		want string
	}{
		"um cliente":                  {[]string{"203.0.113.9"}, "203.0.113.9"},
		"o cliente forjou a esquerda": {[]string{"9.9.9.9, 203.0.113.9"}, "203.0.113.9"}, // vale a entrada mais à direita
		"proxy confiável na cadeia":   {[]string{"203.0.113.9, 10.1.2.3"}, "203.0.113.9"},
		"várias linhas do cabeçalho":  {[]string{"9.9.9.9", "203.0.113.9"}, "203.0.113.9"},
		"só proxies confiáveis":       {[]string{"10.1.1.1, 10.2.2.2"}, "172.30.0.10"},
		"entrada inválida na direita": {[]string{"203.0.113.9, lixo"}, "172.30.0.10"},
		"sem cabeçalho":               {nil, "172.30.0.10"},
	}
	for name, c := range cases {
		if got := ip(req("172.30.0.10:4444", c.xff...)); got != c.want {
			t.Errorf("%s: %s, esperado %s", name, got, c.want)
		}
	}
}

func TestClientIP_IPv6AndMappedAddresses(t *testing.T) {
	ip := middleware.NewIPResolver(trusted("172.30.0.10/32"), "X-Forwarded-For")
	if got := ip(req("[2001:db8::1]:80")); got != "2001:db8::1" {
		t.Errorf("IPv6: %s", got)
	}
	if got := ip(req("[::ffff:172.30.0.10]:80", "203.0.113.9")); got != "203.0.113.9" {
		t.Errorf("IPv4 mapeado em IPv6 deve ser reconhecido como o proxy: %s", got)
	}
}

func TestClientIP_HeaderNameIsConfigurable(t *testing.T) {
	ip := middleware.NewIPResolver(trusted("172.30.0.10/32"), "Fly-Client-IP")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.30.0.10:1"
	r.Header.Set("Fly-Client-IP", "203.0.113.9")
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	if got := ip(r); got != "203.0.113.9" {
		t.Fatalf("got %s", got)
	}
}
