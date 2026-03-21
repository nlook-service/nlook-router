package server

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/health", s.healthHandler)
	s.mux.HandleFunc("/status", s.statusHandler)
	s.mux.HandleFunc("/tools", s.toolsHandler)
	s.mux.HandleFunc("/ai-search", s.aiSearchHandler)
	s.mux.HandleFunc("/ai-warmup", s.aiWarmupHandler)
	s.mux.HandleFunc("/status/model", s.modelStatusHandler)
	s.mux.HandleFunc("/sessions", s.sessionsHandler)
	s.mux.HandleFunc("/sessions/", s.sessionDetailHandler)
	s.mux.HandleFunc("/system/resources", s.systemResourcesHandler)
}
