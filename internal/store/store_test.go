package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// testStore es la instancia compartida inicializada en TestMain.
var testStore *PostgresStore

func TestMain(m *testing.M) {
	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		connString = "postgres://minipaas:minipaas@localhost:5432/minipaas?sslmode=disable"
	}

	ctx := context.Background()
	s, err := NewPostgresStore(ctx, connString)
	if err != nil {
		panic("no se pudo conectar a postgres: " + err.Error())
	}
	testStore = s
	defer s.Close()

	// Limpiar estado previo antes de la suite.
	truncateAll(ctx)

	os.Exit(m.Run())
}

// truncateAll limpia todas las tablas en cascada.
func truncateAll(ctx context.Context) {
	if _, err := testStore.pool.Exec(ctx, "TRUNCATE apps CASCADE"); err != nil {
		panic("truncate fallido: " + err.Error())
	}
}

// cleanup registra un t.Cleanup que trunca al finalizar el test.
func cleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		truncateAll(context.Background())
	})
}

// --- Apps ---

func TestCreateApp_HappyPath(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	app, err := testStore.CreateApp(ctx, "mi-app", "https://github.com/org/repo", "/health")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if app.Name != "mi-app" {
		t.Errorf("nombre esperado %q, got %q", "mi-app", app.Name)
	}
	if !app.ID.Valid {
		t.Error("ID debería ser un UUID válido")
	}
}

func TestCreateApp_DuplicateName(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	if _, err := testStore.CreateApp(ctx, "duplicada", "https://github.com/a/b", "/"); err != nil {
		t.Fatalf("primera inserción: %v", err)
	}
	_, err := testStore.CreateApp(ctx, "duplicada", "https://github.com/c/d", "/")
	if err == nil {
		t.Fatal("se esperaba error por nombre duplicado, got nil")
	}
}

func TestGetApp_Existente(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	if _, err := testStore.CreateApp(ctx, "buscable", "https://github.com/x/y", "/status"); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	app, err := testStore.GetApp(ctx, "buscable")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if app.Name != "buscable" {
		t.Errorf("nombre esperado %q, got %q", "buscable", app.Name)
	}
}

func TestGetApp_NoExiste(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	_, err := testStore.GetApp(ctx, "no-existe")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("se esperaba pgx.ErrNoRows, got %v", err)
	}
}

func TestListApps(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	names := []string{"app-uno", "app-dos", "app-tres"}
	for _, n := range names {
		if _, err := testStore.CreateApp(ctx, n, "https://github.com/o/"+n, "/"); err != nil {
			t.Fatalf("CreateApp %q: %v", n, err)
		}
	}
	apps, err := testStore.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != len(names) {
		t.Errorf("se esperaban %d apps, got %d", len(names), len(apps))
	}
}

func TestDeleteApp(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	if _, err := testStore.CreateApp(ctx, "efimera", "https://github.com/e/f", "/"); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := testStore.DeleteApp(ctx, "efimera"); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	_, err := testStore.GetApp(ctx, "efimera")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("app debería haber sido eliminada, got %v", err)
	}
}

// --- Deployments ---

func crearAppDeTest(t *testing.T, nombre string) App {
	t.Helper()
	app, err := testStore.CreateApp(context.Background(), nombre, "https://github.com/test/"+nombre, "/")
	if err != nil {
		t.Fatalf("CreateApp(%q): %v", nombre, err)
	}
	return app
}

func TestCreateDeployment(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	app := crearAppDeTest(t, "deploy-app")
	dep, err := testStore.CreateDeployment(ctx, app.ID, "sha256:abc123")
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if dep.Status != "pending" {
		t.Errorf("status inicial esperado %q, got %q", "pending", dep.Status)
	}
	if dep.AppID != app.ID {
		t.Error("AppID del deployment no coincide con el de la app")
	}
}

func TestGetActiveDeployment_SinRunning(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	app := crearAppDeTest(t, "sin-running")
	// Crear deployment en estado pending (no running).
	dep, err := testStore.CreateDeployment(ctx, app.ID, "sha256:pending")
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	// Asegurarse de que sigue en pending.
	if dep.Status != "pending" {
		t.Fatalf("status esperado pending, got %q", dep.Status)
	}

	_, err = testStore.GetActiveDeployment(ctx, app.ID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("sin deployment running se esperaba pgx.ErrNoRows, got %v", err)
	}
}

func TestUpdateDeploymentStatus(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	app := crearAppDeTest(t, "update-app")
	dep, err := testStore.CreateDeployment(ctx, app.ID, "sha256:update")
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	params := UpdateDeploymentParams{
		ID:     dep.ID,
		Status: "running",
		ContainerID: pgtype.Text{
			String: "container-abc",
			Valid:  true,
		},
		InternalPort: pgtype.Int4{
			Int32: 8080,
			Valid:  true,
		},
	}
	updated, err := testStore.UpdateDeploymentStatus(ctx, params)
	if err != nil {
		t.Fatalf("UpdateDeploymentStatus: %v", err)
	}
	if updated.Status != "running" {
		t.Errorf("status esperado %q, got %q", "running", updated.Status)
	}
	if !updated.ContainerID.Valid || updated.ContainerID.String != "container-abc" {
		t.Errorf("ContainerID esperado %q, got %+v", "container-abc", updated.ContainerID)
	}
	if !updated.InternalPort.Valid || updated.InternalPort.Int32 != 8080 {
		t.Errorf("InternalPort esperado 8080, got %+v", updated.InternalPort)
	}
}

func TestGetActiveDeployment_ConRunning(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	app := crearAppDeTest(t, "con-running")
	dep, err := testStore.CreateDeployment(ctx, app.ID, "sha256:running")
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if _, err := testStore.UpdateDeploymentStatus(ctx, UpdateDeploymentParams{
		ID:     dep.ID,
		Status: "running",
	}); err != nil {
		t.Fatalf("UpdateDeploymentStatus a running: %v", err)
	}

	active, err := testStore.GetActiveDeployment(ctx, app.ID)
	if err != nil {
		t.Fatalf("GetActiveDeployment: %v", err)
	}
	if active.ID != dep.ID {
		t.Error("ID del deployment activo no coincide")
	}
}

func TestListDeployments(t *testing.T) {
	cleanup(t)
	ctx := context.Background()

	app := crearAppDeTest(t, "list-deps-app")
	tags := []string{"sha256:v1", "sha256:v2", "sha256:v3"}
	for _, tag := range tags {
		if _, err := testStore.CreateDeployment(ctx, app.ID, tag); err != nil {
			t.Fatalf("CreateDeployment(%q): %v", tag, err)
		}
	}

	deps, err := testStore.ListDeployments(ctx, app.ID)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deps) != len(tags) {
		t.Errorf("se esperaban %d deployments, got %d", len(tags), len(deps))
	}
}
