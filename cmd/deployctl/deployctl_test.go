package main_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// rutaBinario es la ruta al binario compilado para los tests.
var rutaBinario string

// TestMain compila el binario una sola vez antes de ejecutar todos los tests.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "deployctl-test-*")
	if err != nil {
		panic("no se pudo crear directorio temporal: " + err.Error())
	}
	defer os.RemoveAll(dir)

	binName := "deployctl-test"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	rutaBinario = filepath.Join(dir, binName)

	// compilar el binario desde la raíz del módulo
	_, thisFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	cmd := exec.Command("go", "build", "-o", rutaBinario, "./cmd/deployctl")
	cmd.Dir = moduleRoot
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("fallo al compilar deployctl: " + err.Error() + "\n" + string(out))
	}

	os.Exit(m.Run())
}

// ejecutar lanza el binario compilado contra un servidor de prueba y retorna stdout, stderr y exit code.
func ejecutar(t *testing.T, srv *httptest.Server, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(rutaBinario, args...)
	cmd.Env = append(os.Environ(), "DEPLOYCTL_API_URL="+srv.URL, "NO_COLOR=1")

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// servidorSimple crea un httptest.Server que responde a una sola ruta con un body JSON.
func servidorSimple(t *testing.T, method, path string, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method || r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if body != nil {
			json.NewEncoder(w).Encode(body) //nolint
		}
	}))
}

// --- tests por comando ---

func TestAppsCreate(t *testing.T) {
	respuesta := map[string]any{
		"id": "abc12345-0000-0000-0000-000000000000", "name": "mi-app",
		"repo_url": "https://github.com/x/y", "health_path": "/", "created_at": "2026-01-01T00:00:00Z",
	}
	srv := servidorSimple(t, "POST", "/apps", http.StatusCreated, respuesta)
	defer srv.Close()

	stdout, stderr, code := ejecutar(t, srv, "apps", "create", "mi-app", "--repo", "https://github.com/x/y")
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "mi-app") {
		t.Errorf("stdout no contiene el nombre de la app: %q", stdout)
	}
}

func TestAppsList(t *testing.T) {
	respuesta := []map[string]any{
		{"id": "abc12345-0000-0000-0000-000000000000", "name": "app-uno", "repo_url": "https://github.com/a/b", "health_path": "/", "created_at": "2026-01-01T00:00:00Z"},
		{"id": "def67890-0000-0000-0000-000000000000", "name": "app-dos", "repo_url": "https://github.com/c/d", "health_path": "/", "created_at": "2026-02-01T00:00:00Z"},
	}
	srv := servidorSimple(t, "GET", "/apps", http.StatusOK, respuesta)
	defer srv.Close()

	stdout, stderr, code := ejecutar(t, srv, "apps", "list")
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "app-uno") || !strings.Contains(stdout, "app-dos") {
		t.Errorf("stdout no contiene las apps esperadas: %q", stdout)
	}
}

func TestAppsListJSON(t *testing.T) {
	respuesta := []map[string]any{
		{"id": "abc12345-0000-0000-0000-000000000000", "name": "app-uno", "repo_url": "https://github.com/a/b", "health_path": "/", "created_at": "2026-01-01T00:00:00Z"},
	}
	srv := servidorSimple(t, "GET", "/apps", http.StatusOK, respuesta)
	defer srv.Close()

	stdout, _, code := ejecutar(t, srv, "apps", "list", "--json")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	// verificar que el output es JSON válido
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Errorf("stdout no es JSON válido: %v\n%s", err, stdout)
	}
}

func TestAppsGet(t *testing.T) {
	respuesta := map[string]any{
		"id": "abc12345-0000-0000-0000-000000000000", "name": "mi-app",
		"repo_url": "https://github.com/x/y", "health_path": "/", "created_at": "2026-01-01T00:00:00Z",
		"active_deployment": nil,
	}
	srv := servidorSimple(t, "GET", "/apps/mi-app", http.StatusOK, respuesta)
	defer srv.Close()

	stdout, stderr, code := ejecutar(t, srv, "apps", "get", "mi-app")
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "mi-app") {
		t.Errorf("stdout no contiene el nombre: %q", stdout)
	}
}

func TestAppsDelete(t *testing.T) {
	srv := servidorSimple(t, "DELETE", "/apps/mi-app", http.StatusNoContent, nil)
	defer srv.Close()

	stdout, stderr, code := ejecutar(t, srv, "apps", "delete", "mi-app")
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "mi-app") {
		t.Errorf("stdout no menciona el nombre de la app: %q", stdout)
	}
}

func TestAppsDeployExitoso(t *testing.T) {
	respuesta := map[string]any{
		"id": "dep12345-0000-0000-0000-000000000000", "app_id": "abc12345-0000-0000-0000-000000000000",
		"image_tag": "mi-app:1234", "status": "running",
		"created_at": "2026-01-01T00:00:00Z",
	}
	srv := servidorSimple(t, "POST", "/apps/mi-app/deployments", http.StatusOK, respuesta)
	defer srv.Close()

	stdout, stderr, code := ejecutar(t, srv, "apps", "deploy", "mi-app")
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "running") {
		t.Errorf("stdout no contiene 'running': %q", stdout)
	}
}

func TestAppsDeployFallido(t *testing.T) {
	errorMsg := "git clone falló"
	respuesta := map[string]any{
		"id": "dep12345-0000-0000-0000-000000000000", "app_id": "abc12345-0000-0000-0000-000000000000",
		"image_tag": "mi-app:1234", "status": "failed",
		"created_at": "2026-01-01T00:00:00Z", "error_message": errorMsg,
	}
	srv := servidorSimple(t, "POST", "/apps/mi-app/deployments", http.StatusOK, respuesta)
	defer srv.Close()

	_, stderr, code := ejecutar(t, srv, "apps", "deploy", "mi-app")
	if code == 0 {
		t.Fatal("se esperaba exit code != 0 para deploy fallido")
	}
	if !strings.Contains(stderr, errorMsg) {
		t.Errorf("stderr no contiene el mensaje de error: %q", stderr)
	}
}

func TestAppsDeployAppNoExiste(t *testing.T) {
	srv := servidorSimple(t, "POST", "/apps/no-existe/deployments", http.StatusNotFound,
		map[string]string{"error": "app no encontrada"})
	defer srv.Close()

	_, stderr, code := ejecutar(t, srv, "apps", "deploy", "no-existe")
	if code == 0 {
		t.Fatal("se esperaba exit code != 0")
	}
	if !strings.Contains(stderr, "app no encontrada") {
		t.Errorf("stderr no contiene el mensaje esperado: %q", stderr)
	}
}

func TestDeploymentsList(t *testing.T) {
	respuesta := []map[string]any{
		{"id": "dep12345-0000-0000-0000-000000000000", "app_id": "abc12345-0000-0000-0000-000000000000",
			"image_tag": "mi-app:1234", "status": "running", "created_at": "2026-01-01T00:00:00Z"},
	}
	srv := servidorSimple(t, "GET", "/apps/mi-app/deployments", http.StatusOK, respuesta)
	defer srv.Close()

	stdout, stderr, code := ejecutar(t, srv, "deployments", "list", "mi-app")
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dep12345") {
		t.Errorf("stdout no contiene el ID del deployment: %q", stdout)
	}
}

func TestDeploymentsGet(t *testing.T) {
	depID := "dep12345-0000-0000-0000-000000000000"
	respuesta := map[string]any{
		"id": depID, "app_id": "abc12345-0000-0000-0000-000000000000",
		"image_tag": "mi-app:1234", "status": "running", "created_at": "2026-01-01T00:00:00Z",
	}
	srv := servidorSimple(t, "GET", "/apps/mi-app/deployments/"+depID, http.StatusOK, respuesta)
	defer srv.Close()

	stdout, stderr, code := ejecutar(t, srv, "deployments", "get", "mi-app", depID)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, depID) {
		t.Errorf("stdout no contiene el ID: %q", stdout)
	}
}

func TestErrorServidor(t *testing.T) {
	srv := servidorSimple(t, "GET", "/apps/no-existe", http.StatusNotFound,
		map[string]string{"error": "app no encontrada"})
	defer srv.Close()

	_, stderr, code := ejecutar(t, srv, "apps", "get", "no-existe")
	if code == 0 {
		t.Fatal("se esperaba exit code != 0")
	}
	if !strings.Contains(stderr, "app no encontrada") {
		t.Errorf("stderr no contiene el mensaje de error: %q", stderr)
	}
}
