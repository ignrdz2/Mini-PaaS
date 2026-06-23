package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ignrdz2/mini-paas/internal/deploy"
	"github.com/ignrdz2/mini-paas/internal/docker"
	"github.com/ignrdz2/mini-paas/internal/proxy"
	"github.com/ignrdz2/mini-paas/internal/store"
)

// Server agrupa las dependencias del servidor HTTP y el router.
type Server struct {
	store        store.Store
	orchestrator *deploy.Orchestrator
	docker       *docker.DockerClient
	proxy        proxy.ProxyManager
	router       *chi.Mux
}

// NewServer crea el servidor e inicializa el router con todos los endpoints.
func NewServer(s store.Store, o *deploy.Orchestrator, d *docker.DockerClient, p proxy.ProxyManager) *Server {
	srv := &Server{
		store:        s,
		orchestrator: o,
		docker:       d,
		proxy:        p,
		router:       chi.NewRouter(),
	}
	srv.registrarRutas()
	return srv
}

// registrarRutas configura el middleware y los siete endpoints de la API.
func (s *Server) registrarRutas() {
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	s.router.Post("/apps", s.crearApp)
	s.router.Get("/apps", s.listarApps)

	s.router.Route("/apps/{name}", func(r chi.Router) {
		r.Get("/", s.obtenerApp)
		r.Delete("/", s.eliminarApp)
		r.Post("/deployments", s.crearDeployment)
		r.Get("/deployments", s.listarDeployments)
		r.Get("/deployments/{id}", s.obtenerDeployment)
	})
}

// ServeHTTP delega la petición al router de chi.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
