package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// IPResolver descobre o IP do CLIENTE de uma requisição.
type IPResolver func(*http.Request) string

// NewIPResolver implementa a regra do IP real atrás de proxy (spec §7.4):
//  1. Se a conexão NÃO vem de um proxy confiável, o IP é o da conexão e o cabeçalho é IGNORADO
//     (qualquer cliente poderia forjá-lo).
//  2. Se vem, lemos as entradas do cabeçalho da DIREITA para a ESQUERDA, pulando proxies
//     confiáveis; a primeira que não é proxy é o cliente. Nunca usamos a primeira entrada
//     (a mais à esquerda é a que o cliente controla).
//  3. Entrada que não é IP válido, ou cabeçalho ausente: usamos o IP da conexão.
func NewIPResolver(trusted []netip.Prefix, header string) IPResolver {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	return func(r *http.Request) string {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		conn, err := netip.ParseAddr(host)
		if err != nil {
			return host
		}
		conn = conn.Unmap() // "::ffff:1.2.3.4" é o IPv4 1.2.3.4
		if !isTrusted(conn) {
			return conn.String()
		}
		var entries []string
		for _, v := range r.Header.Values(header) {
			for _, part := range strings.Split(v, ",") {
				entries = append(entries, strings.TrimSpace(part))
			}
		}
		for i := len(entries) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(entries[i])
			if err != nil {
				return conn.String()
			}
			a = a.Unmap()
			if isTrusted(a) {
				continue
			}
			return a.String()
		}
		return conn.String()
	}
}
