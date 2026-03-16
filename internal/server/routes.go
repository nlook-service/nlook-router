package server

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/health", s.healthHandler)
	s.mux.HandleFunc("/status", s.statusHandler)
}
