package server

import "net/http"

// authMiddleware protects the API. Etapa 2 (Agente S1) substitui este pass-through com CORS
// pelo token de sessão, verificação de Origin e remoção do CORS conforme o contrato 1.1.
func (s *AppServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
