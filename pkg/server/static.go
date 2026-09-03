package server

import "net/http"

// uiHandler serves the embedded UI. Etapa 2 (Agente S1) substitui esta implementação
// para injetar o token de sessão no lugar do placeholder {{SCANFILE_TOKEN}} do index.html
// (contrato 1.1). Mantida em arquivo próprio para não colidir com lifecycle.go.
func (s *AppServer) uiHandler() http.Handler {
	if s.uiFS == nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(s.uiFS))
}
