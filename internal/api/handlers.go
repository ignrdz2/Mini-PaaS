package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ignrdz2/mini-paas/internal/deploy"
	"github.com/ignrdz2/mini-paas/internal/proxy"
	"github.com/ignrdz2/mini-paas/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- tipos de request y respuesta ---

type crearAppRequest struct {
	Name       string `json:"name"`
	RepoURL    string `json:"repo_url"`
	HealthPath string `json:"health_path"`
}

// appResponse es la representación JSON de una app.
type appResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	RepoURL    string `json:"repo_url"`
	HealthPath string `json:"health_path"`
	CreatedAt  string `json:"created_at"`
}

// appConDeployment extiende appResponse con el deployment activo (puede ser null).
type appConDeployment struct {
	appResponse
	ActiveDeployment *deploymentResponse `json:"active_deployment"`
}

// deploymentResponse es la representación JSON de un deployment.
type deploymentResponse struct {
	ID           string  `json:"id"`
	AppID        string  `json:"app_id"`
	ImageTag     string  `json:"image_tag"`
	Status       string  `json:"status"`
	ContainerID  *string `json:"container_id"`
	InternalPort *int32  `json:"internal_port"`
	CreatedAt    string  `json:"created_at"`
	FinishedAt   *string `json:"finished_at"`
	ErrorMessage *string `json:"error_message"`
}

// --- helpers de serialización ---

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func timestampStr(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func toAppResponse(a store.App) appResponse {
	return appResponse{
		ID:         uuidStr(a.ID),
		Name:       a.Name,
		RepoURL:    a.RepoUrl,
		HealthPath: a.HealthPath,
		CreatedAt:  timestampStr(a.CreatedAt),
	}
}

func toDeploymentResponse(d store.Deployment) deploymentResponse {
	r := deploymentResponse{
		ID:        uuidStr(d.ID),
		AppID:     uuidStr(d.AppID),
		ImageTag:  d.ImageTag,
		Status:    d.Status,
		CreatedAt: timestampStr(d.CreatedAt),
	}
	if d.ContainerID.Valid {
		s := d.ContainerID.String
		r.ContainerID = &s
	}
	if d.InternalPort.Valid {
		v := d.InternalPort.Int32
		r.InternalPort = &v
	}
	if d.FinishedAt.Valid {
		s := d.FinishedAt.Time.UTC().Format(time.RFC3339)
		r.FinishedAt = &s
	}
	if d.ErrorMessage.Valid {
		s := d.ErrorMessage.String
		r.ErrorMessage = &s
	}
	return r
}

// --- helpers HTTP ---

func responderJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint
}

func responderError(w http.ResponseWriter, status int, msg string) {
	responderJSON(w, status, map[string]string{"error": msg})
}

// isDuplicateKey retorna true si el error corresponde al código Postgres 23505 (clave duplicada).
func isDuplicateKey(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

// --- handlers ---

// crearApp maneja POST /apps.
func (s *Server) crearApp(w http.ResponseWriter, r *http.Request) {
	var req crearAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderError(w, http.StatusBadRequest, "cuerpo JSON inválido")
		return
	}
	if req.Name == "" || req.RepoURL == "" {
		responderError(w, http.StatusBadRequest, "name y repo_url son requeridos")
		return
	}
	if req.HealthPath == "" {
		req.HealthPath = "/"
	}

	app, err := s.store.CreateApp(r.Context(), req.Name, req.RepoURL, req.HealthPath)
	if err != nil {
		if isDuplicateKey(err) {
			responderError(w, http.StatusConflict, "ya existe una app con ese nombre")
			return
		}
		responderError(w, http.StatusInternalServerError, "error al crear la app")
		return
	}

	responderJSON(w, http.StatusCreated, toAppResponse(app))
}

// listarApps maneja GET /apps.
func (s *Server) listarApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.ListApps(r.Context())
	if err != nil {
		responderError(w, http.StatusInternalServerError, "error al listar apps")
		return
	}
	result := make([]appResponse, len(apps))
	for i, a := range apps {
		result[i] = toAppResponse(a)
	}
	responderJSON(w, http.StatusOK, result)
}

// obtenerApp maneja GET /apps/{name}.
func (s *Server) obtenerApp(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	app, err := s.store.GetApp(r.Context(), name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responderError(w, http.StatusNotFound, "app no encontrada")
			return
		}
		responderError(w, http.StatusInternalServerError, "error al obtener la app")
		return
	}

	resp := appConDeployment{appResponse: toAppResponse(app)}

	active, err := s.store.GetActiveDeployment(r.Context(), app.ID)
	if err == nil {
		d := toDeploymentResponse(active)
		resp.ActiveDeployment = &d
	}

	responderJSON(w, http.StatusOK, resp)
}

// eliminarApp maneja DELETE /apps/{name}.
func (s *Server) eliminarApp(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	app, err := s.store.GetApp(r.Context(), name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responderError(w, http.StatusNotFound, "app no encontrada")
			return
		}
		responderError(w, http.StatusInternalServerError, "error al obtener la app")
		return
	}

	// detener el container activo si existe antes de eliminar la app
	if active, err := s.store.GetActiveDeployment(r.Context(), app.ID); err == nil {
		if active.ContainerID.Valid {
			s.docker.StopAndRemoveContainer(context.Background(), active.ContainerID.String) //nolint
		}
		now := time.Now()
		s.store.UpdateDeploymentStatus(r.Context(), store.UpdateDeploymentParams{ //nolint
			ID:         active.ID,
			Status:     "stopped",
			FinishedAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
	}

	if err := s.store.DeleteApp(r.Context(), name); err != nil {
		responderError(w, http.StatusInternalServerError, "error al eliminar la app")
		return
	}

	// reflejar la eliminación en el proxy
	s.sincronizarProxy(r.Context()) //nolint

	w.WriteHeader(http.StatusNoContent)
}

// crearDeployment maneja POST /apps/{name}/deployments.
func (s *Server) crearDeployment(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	dep, err := s.orchestrator.Deploy(ctx, name)
	if err != nil {
		if errors.Is(err, deploy.ErrAppNotFound) {
			responderError(w, http.StatusNotFound, "app no encontrada")
			return
		}
		// si el deployment fue creado pero falló, retornarlo con 200 para que el cliente
		// pueda leer el error_message en lugar de recibir un 500 opaco
		if dep.ID.Valid {
			responderJSON(w, http.StatusOK, toDeploymentResponse(dep))
			return
		}
		responderError(w, http.StatusInternalServerError, "error al iniciar el deployment")
		return
	}

	responderJSON(w, http.StatusOK, toDeploymentResponse(dep))
}

// listarDeployments maneja GET /apps/{name}/deployments.
func (s *Server) listarDeployments(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	app, err := s.store.GetApp(r.Context(), name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responderError(w, http.StatusNotFound, "app no encontrada")
			return
		}
		responderError(w, http.StatusInternalServerError, "error al obtener la app")
		return
	}

	deps, err := s.store.ListDeployments(r.Context(), app.ID)
	if err != nil {
		responderError(w, http.StatusInternalServerError, "error al listar deployments")
		return
	}

	result := make([]deploymentResponse, len(deps))
	for i, d := range deps {
		result[i] = toDeploymentResponse(d)
	}
	responderJSON(w, http.StatusOK, result)
}

// obtenerDeployment maneja GET /apps/{name}/deployments/{id}.
func (s *Server) obtenerDeployment(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")

	var uid pgtype.UUID
	if err := uid.Scan(idStr); err != nil {
		responderError(w, http.StatusBadRequest, "id de deployment inválido")
		return
	}

	dep, err := s.store.GetDeployment(r.Context(), uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responderError(w, http.StatusNotFound, "deployment no encontrado")
			return
		}
		responderError(w, http.StatusInternalServerError, "error al obtener el deployment")
		return
	}

	responderJSON(w, http.StatusOK, toDeploymentResponse(dep))
}

// sincronizarProxy reconstruye las routes activas de todas las apps y actualiza el ProxyManager.
func (s *Server) sincronizarProxy(ctx context.Context) error {
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return err
	}
	var routes []proxy.Route
	for _, app := range apps {
		active, err := s.store.GetActiveDeployment(ctx, app.ID)
		if err == nil && active.InternalPort.Valid {
			routes = append(routes, proxy.Route{
				AppName:    app.Name,
				TargetPort: int(active.InternalPort.Int32),
			})
		}
	}
	return s.proxy.Sync(ctx, routes)
}
