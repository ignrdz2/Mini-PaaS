package deploy_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ignrdz2/mini-paas/internal/builder"
	"github.com/ignrdz2/mini-paas/internal/deploy"
	"github.com/ignrdz2/mini-paas/internal/proxy"
	"github.com/ignrdz2/mini-paas/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- stubs de las interfaces ---

// stubStore implementa store.Store en memoria para tests unitarios.
type stubStore struct {
	app         store.App
	appErr      error
	deployments []store.Deployment
	nextDepID   pgtype.UUID
}

func (s *stubStore) GetApp(_ context.Context, _ string) (store.App, error) {
	return s.app, s.appErr
}

func (s *stubStore) CreateApp(_ context.Context, _, _, _ string) (store.App, error) {
	return store.App{}, nil
}

func (s *stubStore) ListApps(_ context.Context) ([]store.App, error) {
	return []store.App{s.app}, nil
}

func (s *stubStore) DeleteApp(_ context.Context, _ string) error { return nil }

func (s *stubStore) CreateDeployment(_ context.Context, appID pgtype.UUID, imageTag string) (store.Deployment, error) {
	dep := store.Deployment{
		ID:       s.nextDepID,
		AppID:    appID,
		ImageTag: imageTag,
		Status:   "pending",
	}
	s.deployments = append(s.deployments, dep)
	return dep, nil
}

func (s *stubStore) GetDeployment(_ context.Context, id pgtype.UUID) (store.Deployment, error) {
	for _, d := range s.deployments {
		if d.ID == id {
			return d, nil
		}
	}
	return store.Deployment{}, pgx.ErrNoRows
}

func (s *stubStore) ListDeployments(_ context.Context, _ pgtype.UUID) ([]store.Deployment, error) {
	return s.deployments, nil
}

func (s *stubStore) GetActiveDeployment(_ context.Context, _ pgtype.UUID) (store.Deployment, error) {
	// buscar el deployment más reciente con status running
	for i := len(s.deployments) - 1; i >= 0; i-- {
		if s.deployments[i].Status == "running" {
			return s.deployments[i], nil
		}
	}
	return store.Deployment{}, pgx.ErrNoRows
}

func (s *stubStore) UpdateDeploymentStatus(_ context.Context, params store.UpdateDeploymentParams) (store.Deployment, error) {
	for i := range s.deployments {
		if s.deployments[i].ID == params.ID {
			s.deployments[i].Status = params.Status
			if params.ContainerID.Valid {
				s.deployments[i].ContainerID = params.ContainerID
			}
			if params.InternalPort.Valid {
				s.deployments[i].InternalPort = params.InternalPort
			}
			if params.FinishedAt.Valid {
				s.deployments[i].FinishedAt = params.FinishedAt
			}
			if params.ErrorMessage.Valid {
				s.deployments[i].ErrorMessage = params.ErrorMessage
			}
			return s.deployments[i], nil
		}
	}
	return store.Deployment{}, pgx.ErrNoRows
}

// stubBuilder implementa builder.Builder para tests.
type stubBuilder struct {
	err error
}

func (b *stubBuilder) Build(_ context.Context, _, imageTag string) (builder.BuildResult, error) {
	if b.err != nil {
		return builder.BuildResult{}, b.err
	}
	return builder.BuildResult{ImageTag: imageTag, Logs: "build ok"}, nil
}

// stubDocker simula las operaciones de runtime de containers.
type stubDocker struct {
	runErr    error
	containerID string
	port      int
	stopped   []string // IDs de containers detenidos
}

func (d *stubDocker) RunContainer(_ context.Context, _, _ string) (string, int, error) {
	if d.runErr != nil {
		return "", 0, d.runErr
	}
	return d.containerID, d.port, nil
}

func (d *stubDocker) StopAndRemoveContainer(_ context.Context, containerID string) error {
	d.stopped = append(d.stopped, containerID)
	return nil
}

// stubProxy implementa proxy.ProxyManager para tests.
type stubProxy struct {
	syncErr    error
	lastRoutes []proxy.Route
}

func (p *stubProxy) Sync(_ context.Context, routes []proxy.Route) error {
	p.lastRoutes = routes
	return p.syncErr
}

// stubOrchestrator agrupa los stubs para construir un Orchestrator de test.
// Usa un DockerClientFacade para poder pasar stubs sin exponer el *docker.DockerClient real.
type testDeps struct {
	s  *stubStore
	b  *stubBuilder
	d  *stubDocker
	p  *stubProxy
}

// --- helpers para crear Orchestrators de test ---

// La función NewOrchestrator acepta *docker.DockerClient concreto, así que necesitamos
// una alternativa para tests. Definimos una interfaz mínima local y exponemos un constructor
// de test vía la función interna del paquete.
// Como estamos en package deploy_test (externo), usamos el constructor público con un
// DockerClient real solo para el happy-path que no llega a llamar a Docker
// (el stub maneja las llamadas reales interceptando en otro nivel).
//
// Para evitar dependencia de Docker en estos tests unitarios puros, exponemos
// NewOrchestratorWithDocker en el paquete deploy (ver orchestration.go) que acepta
// la interfaz dockerRunner.

func nuevoDeps() testDeps {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	depID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	return testDeps{
		s: &stubStore{
			app: store.App{
				ID:         appID,
				Name:       "test-app",
				RepoUrl:    "https://github.com/ignrdz2/mini-paas",
				HealthPath: "/",
			},
			nextDepID: depID,
		},
		b: &stubBuilder{},
		d: &stubDocker{containerID: "container-abc", port: 9999},
		p: &stubProxy{},
	}
}

// --- tests ---

func TestDeploy_HappyPath_SecuenciaDeEstados(t *testing.T) {
	// levantar un servidor HTTP real para que WaitHealthy lo detecte como healthy
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	port := puertoDeURL(t, srv.URL)

	deps := nuevoDeps()
	deps.d.port = port
	orch := deploy.NewOrchestratorWithRunner(deps.s, deps.b, deps.d, deps.p)

	dep, err := orch.Deploy(context.Background(), "test-app")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if dep.Status != "running" {
		t.Errorf("status final esperado 'running', got %q", dep.Status)
	}

	// verificar que se registró el containerID y el port
	if !dep.ContainerID.Valid || dep.ContainerID.String != "container-abc" {
		t.Errorf("ContainerID esperado 'container-abc', got %+v", dep.ContainerID)
	}
	if !dep.InternalPort.Valid || dep.InternalPort.Int32 != int32(port) {
		t.Errorf("InternalPort esperado %d, got %+v", port, dep.InternalPort)
	}

	// proxy debe haber recibido al menos una route
	if len(deps.p.lastRoutes) == 0 {
		t.Error("proxy.Sync no recibió ninguna route")
	}
}

func TestDeploy_AppNoExiste(t *testing.T) {
	deps := nuevoDeps()
	deps.s.appErr = pgx.ErrNoRows
	orch := deploy.NewOrchestratorWithRunner(deps.s, deps.b, deps.d, deps.p)

	_, err := orch.Deploy(context.Background(), "no-existe")
	if err == nil {
		t.Fatal("se esperaba error para app inexistente")
	}
	if !errors.Is(err, deploy.ErrAppNotFound) {
		t.Errorf("se esperaba ErrAppNotFound, got: %v", err)
	}
}

func TestDeploy_FalloBuild_EstadoFailed(t *testing.T) {
	deps := nuevoDeps()
	deps.b.err = errors.New("imagen base no encontrada")
	orch := deploy.NewOrchestratorWithRunner(deps.s, deps.b, deps.d, deps.p)

	dep, err := orch.Deploy(context.Background(), "test-app")
	if err == nil {
		t.Fatal("se esperaba error de build")
	}

	// el deployment debe quedar en failed con el mensaje de error
	if dep.Status != "failed" {
		t.Errorf("status esperado 'failed', got %q", dep.Status)
	}
	if !dep.ErrorMessage.Valid || dep.ErrorMessage.String == "" {
		t.Error("ErrorMessage debería estar poblado tras fallo de build")
	}
}

func TestDeploy_FalloHealthcheck_ContainerDetenido(t *testing.T) {
	deps := nuevoDeps()
	// el container se arranca (port 1 — ningún servidor escucha ahí) y el healthcheck falla
	deps.d.port = 1
	// timeout muy corto para no hacer lento el test
	orch := deploy.NewOrchestratorWithRunner(deps.s, deps.b, deps.d, deps.p,
		deploy.WithHealthTimeout(500*time.Millisecond))

	dep, err := orch.Deploy(context.Background(), "test-app")
	if err == nil {
		t.Fatal("se esperaba error de healthcheck")
	}
	if dep.Status != "failed" {
		t.Errorf("status esperado 'failed', got %q", dep.Status)
	}
	// el container debe haber sido detenido
	if len(deps.d.stopped) == 0 {
		t.Error("se esperaba que StopAndRemoveContainer fuera llamado tras fallo de healthcheck")
	}
}

func TestDeploy_FalloProxy_NoAfectaResultado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	deps := nuevoDeps()
	deps.d.port = puertoDeURL(t, srv.URL)
	deps.p.syncErr = errors.New("traefik no disponible")
	orch := deploy.NewOrchestratorWithRunner(deps.s, deps.b, deps.d, deps.p)

	dep, err := orch.Deploy(context.Background(), "test-app")
	// el deploy debe ser exitoso aunque el proxy falle
	if err != nil {
		t.Fatalf("fallo de proxy no debería fallar el deploy, got: %v", err)
	}
	if dep.Status != "running" {
		t.Errorf("status esperado 'running', got %q", dep.Status)
	}
}
