// Um ResponseWriter que lembra o status escrito.

package middleware

import "net/http"

// statusRecorder guarda o status escrito, para o log, as métricas e o recovery.
//
// Ele embute o http.ResponseWriter original (o campo sem nome). Embutir uma
// interface promove só os métodos dela (Header, Write, WriteHeader); outros
// que o writer concreto tenha, como Flush, ficam escondidos, e por isso
// existe o Unwrap abaixo. Aqui sobrescrevemos WriteHeader e Write.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// WriteHeader anota o primeiro status escrito e repassa.
func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status, s.wrote = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

// Write sem WriteHeader antes quer dizer 200: é o que o net/http faz.
func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status, s.wrote = http.StatusOK, true
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap deixa http.ResponseController alcançar o ResponseWriter de verdade.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Status devolve o status da resposta; 200 se nada foi escrito.
func (s *statusRecorder) Status() int {
	if !s.wrote {
		return http.StatusOK
	}
	return s.status
}
