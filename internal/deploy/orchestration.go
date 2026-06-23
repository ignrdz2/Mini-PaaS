package deploy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/ignrdz2/mini-paas/internal/builder"
	"github.com/ignrdz2/mini-paas/internal/docker"
	"github.com/ignrdz2/mini-paas/internal/proxy"
	"github.com/ignrdz2/mini-paas/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrAppNotFound se retorna cuando la app solicitada no existe en el store.
var ErrAppNotFound = errors.New("app no encontrada")

// dockerRunner es la interfaz mínima de Docker que usa el Orchestrator.
// Permite sustituir el cliente real por un stub en tests unitarios.
type dockerRunner interface {
	RunContainer(ctx context.Context, imageTag, appName string) (string, int, error)
	StopAndRemoveContainer(ctx context.Context, containerID string) error
}

// Orchestrator coordina las piezas del sistema para realizar un deploy completo.
type Orchestrator struct {
	store         store.Store
	builder       builder.Builder
	docker        dockerRunner
	proxy         proxy.ProxyManager
	healthTimeout time.Duration
	healthHost    string
}

// Option permite configurar opciones opcionales del Orchestrator.
type Option func(*Orchestrator)

// WithHealthTimeout sobreescribe el timeout de healthcheck (útil en tests).
func WithHealthTimeout(d time.Duration) Option {
	return func(o *Orchestrator) { o.healthTimeout = d }
}

// WithHealthHost sobreescribe el host que se usa para el healthcheck.
// Usar "host.docker.internal" cuando el orquestador corre dentro de Docker Desktop (Windows/Mac).
func WithHealthHost(host string) Option {
	return func(o *Orchestrator) { o.healthHost = host }
}

func orquestadorBase(s store.Store, b builder.Builder, d dockerRunner, p proxy.ProxyManager) *Orchestrator {
	return &Orchestrator{
		store:         s,
		builder:       b,
		docker:        d,
		proxy:         p,
		healthTimeout: 30 * time.Second,
		healthHost:    "localhost",
	}
}

// NewOrchestrator crea un Orchestrator con un timeout de healthcheck de 30s por defecto.
func NewOrchestrator(s store.Store, b builder.Builder, d *docker.DockerClient, p proxy.ProxyManager, opts ...Option) *Orchestrator {
	o := orquestadorBase(s, b, d, p)
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewOrchestratorWithRunner es el constructor de test que acepta la interfaz dockerRunner
// en lugar del *docker.DockerClient concreto. Solo debe usarse en tests.
func NewOrchestratorWithRunner(s store.Store, b builder.Builder, d dockerRunner, p proxy.ProxyManager, opts ...Option) *Orchestrator {
	o := orquestadorBase(s, b, d, p)
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Deploy ejecuta el flujo completo de despliegue para appName.
// Sigue la secuencia: pending → clone → building → run → healthcheck → running,
// y detiene el deployment anterior si existía uno en estado running.
func (o *Orchestrator) Deploy(ctx context.Context, appName string) (store.Deployment, error) {
	// 1. obtener la app; fallar con error identificable si no existe
	app, err := o.store.GetApp(ctx, appName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Deployment{}, fmt.Errorf("%w: %s", ErrAppNotFound, appName)
		}
		return store.Deployment{}, fmt.Errorf("error al obtener app %q: %w", appName, err)
	}

	imageTag := fmt.Sprintf("%s:%d", appName, time.Now().Unix())

	// 2. crear deployment en estado pending
	dep, err := o.store.CreateDeployment(ctx, app.ID, imageTag)
	if err != nil {
		return store.Deployment{}, fmt.Errorf("error al crear deployment para %q: %w", appName, err)
	}

	// 3. clonar el repo en un directorio temporal
	tmpDir, err := os.MkdirTemp("", "mini-paas-clone-*")
	if err != nil {
		return o.fallarDeployment(ctx, dep, fmt.Sprintf("error al crear directorio temporal: %v", err))
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", app.RepoUrl, tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := truncar4000(fmt.Sprintf("git clone falló: %v\n%s", err, out))
		return o.fallarDeployment(ctx, dep, msg)
	}

	// 4. transición a building → ejecutar build
	dep, err = o.actualizarEstado(ctx, dep.ID, "building", actualizarOpts{})
	if err != nil {
		return store.Deployment{}, err
	}

	if _, err := o.builder.Build(ctx, tmpDir, imageTag); err != nil {
		return o.fallarDeployment(ctx, dep, truncar4000(err.Error()))
	}

	// 5. arrancar el container
	containerID, port, err := o.docker.RunContainer(ctx, imageTag, appName)
	if err != nil {
		return o.fallarDeployment(ctx, dep, truncar4000(err.Error()))
	}

	// 6. transición a healthcheck → esperar que la app responda
	dep, err = o.actualizarEstado(ctx, dep.ID, "healthcheck", actualizarOpts{
		containerID: containerID,
		port:        port,
	})
	if err != nil {
		o.docker.StopAndRemoveContainer(context.Background(), containerID) //nolint
		return store.Deployment{}, err
	}

	if err := WaitHealthy(ctx, o.healthHost, port, app.HealthPath, o.healthTimeout); err != nil {
		o.docker.StopAndRemoveContainer(context.Background(), containerID) //nolint
		return o.fallarDeployment(ctx, dep, fmt.Sprintf("healthcheck falló: %v", err))
	}

	// 7. transición a running
	now := time.Now()
	dep, err = o.actualizarEstado(ctx, dep.ID, "running", actualizarOpts{finishedAt: &now})
	if err != nil {
		return store.Deployment{}, err
	}

	// 8. detener el deployment anterior running de esta app, si existía
	if old, err := o.store.GetActiveDeployment(ctx, app.ID); err == nil && old.ID != dep.ID {
		if old.ContainerID.Valid {
			o.docker.StopAndRemoveContainer(context.Background(), old.ContainerID.String) //nolint
		}
		finNow := time.Now()
		o.actualizarEstadoPorID(ctx, old.ID, "stopped", actualizarOpts{finishedAt: &finNow}) //nolint
	}

	// 9. sincronizar Traefik con el estado global de todas las apps
	if err := o.sincronizarProxy(ctx); err != nil {
		// fallo de proxy no es fatal — el container ya está corriendo
		log.Printf("advertencia: no se pudo sincronizar el proxy: %v", err)
	}

	// 10. retornar el deployment en estado running
	return dep, nil
}

// --- helpers internos ---

// actualizarOpts agrupa los campos opcionales para UpdateDeploymentStatus.
type actualizarOpts struct {
	containerID string
	port        int
	finishedAt  *time.Time
	errorMsg    string
}

// actualizarEstado aplica una transición de estado al deployment y retorna el estado actualizado.
func (o *Orchestrator) actualizarEstado(ctx context.Context, id pgtype.UUID, status string, opts actualizarOpts) (store.Deployment, error) {
	params := store.UpdateDeploymentParams{ID: id, Status: status}

	if opts.containerID != "" {
		params.ContainerID = pgtype.Text{String: opts.containerID, Valid: true}
	}
	if opts.port > 0 {
		params.InternalPort = pgtype.Int4{Int32: int32(opts.port), Valid: true}
	}
	if opts.finishedAt != nil {
		params.FinishedAt = pgtype.Timestamptz{Time: *opts.finishedAt, Valid: true}
	}
	if opts.errorMsg != "" {
		params.ErrorMessage = pgtype.Text{String: opts.errorMsg, Valid: true}
	}

	dep, err := o.store.UpdateDeploymentStatus(ctx, params)
	if err != nil {
		return store.Deployment{}, fmt.Errorf("error al actualizar estado a %q: %w", status, err)
	}
	return dep, nil
}

// actualizarEstadoPorID es igual que actualizarEstado pero acepta un ID distinto al dep actual.
// Se usa para actualizar deployments anteriores (ej. marcar el viejo como stopped).
func (o *Orchestrator) actualizarEstadoPorID(ctx context.Context, id pgtype.UUID, status string, opts actualizarOpts) (store.Deployment, error) {
	return o.actualizarEstado(ctx, id, status, opts)
}

// fallarDeployment marca el deployment como failed con el mensaje dado y retorna el error como error de Go.
func (o *Orchestrator) fallarDeployment(ctx context.Context, dep store.Deployment, msg string) (store.Deployment, error) {
	now := time.Now()
	updated, _ := o.actualizarEstado(context.Background(), dep.ID, "failed", actualizarOpts{
		errorMsg:   msg,
		finishedAt: &now,
	})
	return updated, fmt.Errorf("%s", msg)
}

// sincronizarProxy obtiene todos los deployments running del sistema y actualiza Traefik.
func (o *Orchestrator) sincronizarProxy(ctx context.Context) error {
	apps, err := o.store.ListApps(ctx)
	if err != nil {
		return fmt.Errorf("error al listar apps para sincronizar proxy: %w", err)
	}

	var routes []proxy.Route
	for _, app := range apps {
		active, err := o.store.GetActiveDeployment(ctx, app.ID)
		if err != nil {
			// esta app no tiene deployment running — omitir
			continue
		}
		if active.InternalPort.Valid {
			routes = append(routes, proxy.Route{
				AppName:    app.Name,
				TargetPort: int(active.InternalPort.Int32),
			})
		}
	}

	return o.proxy.Sync(ctx, routes)
}

// truncar4000 retorna los últimos 4000 caracteres del string dado.
func truncar4000(s string) string {
	const maxLen = 4000
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[len(runes)-maxLen:])
}
