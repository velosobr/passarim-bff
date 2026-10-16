// Credenciais de transporte da conexão gRPC com o catalog: sem criptografia
// na rede privada local, TLS ou mTLS quando configurados.
package grpcclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// TLS: sem CAFile o cliente usa "insecure" (catalog na rede privada local).
// Com CAFile liga TLS; com CertFile+KeyFile também apresenta certificado de
// cliente (a base do mTLS).
//
// TLS cifra a conexão e prova a identidade do servidor: o BFF só confia no
// catalog se o certificado dele foi assinado pela CA informada. No mTLS
// ("TLS mútuo"), o BFF também apresenta um certificado, e o catalog pode
// recusar clientes desconhecidos.
type TLS struct {
	CAFile, CertFile, KeyFile, ServerName string
}

// credentials monta as credenciais a partir dos arquivos. Os erros citam a
// variável de ambiente, e não o caminho, como no resto da configuração. A
// versão mínima é TLS 1.3, a mais nova e segura.
func (t TLS) credentials() (credentials.TransportCredentials, error) {
	if t.CAFile == "" {
		return insecure.NewCredentials(), nil
	}
	pem, err := os.ReadFile(t.CAFile) //nolint:gosec // caminho vem da configuração do operador
	if err != nil {
		return nil, errors.New("CATALOG_TLS_CA_FILE: não foi possível ler o arquivo")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("CATALOG_TLS_CA_FILE: nenhum certificado PEM válido")
	}
	cfg := &tls.Config{RootCAs: pool, ServerName: t.ServerName, MinVersion: tls.VersionTLS13}
	if t.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, errors.New("CATALOG_TLS_CERT_FILE/CATALOG_TLS_KEY_FILE: par de certificado inválido")
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return credentials.NewTLS(cfg), nil
}
