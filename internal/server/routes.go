package server

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/health", s.healthHandler)
	s.mux.HandleFunc("/status", s.statusHandler)
	s.mux.HandleFunc("/tools", s.toolsHandler)
	s.mux.HandleFunc("/ai-search", s.aiSearchHandler)
}
