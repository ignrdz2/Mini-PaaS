package proxy_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ignrdz2/mini-paas/internal/proxy"
	"gopkg.in/yaml.v3"
)

func nuevoManager(t *testing.T) (*proxy.TraefikFileProxyManager, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dynamic.yml")
	return proxy.NewTraefikFileProxyManager(configPath), configPath
}

// leerYAML parsea el archivo generado en un map genérico para inspección.
func leerYAML(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("leerYAML: %v", err)
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("yaml.Unmarshal: %v\ncontenido:\n%s", err, data)
	}
	return out
}

func TestSync_RoutesVacias(t *testing.T) {
	mgr, configPath := nuevoManager(t)

	if err := mgr.Sync(context.Background(), nil); err != nil {
		t.Fatalf("Sync con routes vacías: %v", err)
	}

	// el archivo debe existir y ser YAML válido (puede estar vacío o tener http: null)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("archivo no fue creado: %v", err)
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Errorf("YAML generado no es válido: %v\ncontenido:\n%s", err, data)
	}
}

func TestSync_UnaRoute(t *testing.T) {
	mgr, configPath := nuevoManager(t)

	routes := []proxy.Route{
		{AppName: "mi-app", TargetPort: 9000},
	}
	if err := mgr.Sync(context.Background(), routes); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("leer YAML: %v", err)
	}
	content := string(data)

	// router
	if !strings.Contains(content, "mi-app:") {
		t.Error("falta entrada de router para mi-app")
	}
	if !strings.Contains(content, "PathPrefix(`/mi-app`)") {
		t.Error("falta rule PathPrefix para mi-app")
	}
	// middleware strip prefix
	if !strings.Contains(content, "mi-app-stripprefix:") {
		t.Error("falta middleware mi-app-stripprefix")
	}
	if !strings.Contains(content, "/mi-app") {
		t.Error("falta prefijo /mi-app en stripPrefix")
	}
	// service con puerto correcto
	if !strings.Contains(content, "http://localhost:9000") {
		t.Error("falta url http://localhost:9000 en service")
	}
}

func TestSync_UnaRoute_EstructuraCompleta(t *testing.T) {
	mgr, configPath := nuevoManager(t)

	routes := []proxy.Route{
		{AppName: "app-x", TargetPort: 8765},
	}
	if err := mgr.Sync(context.Background(), routes); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	cfg := leerYAML(t, configPath)

	http, ok := cfg["http"].(map[string]interface{})
	if !ok {
		t.Fatalf("clave 'http' ausente o tipo incorrecto")
	}

	// routers
	routers, ok := http["routers"].(map[string]interface{})
	if !ok {
		t.Fatal("clave 'routers' ausente")
	}
	if _, exists := routers["app-x"]; !exists {
		t.Error("router 'app-x' no encontrado")
	}

	// middlewares
	middlewares, ok := http["middlewares"].(map[string]interface{})
	if !ok {
		t.Fatal("clave 'middlewares' ausente")
	}
	if _, exists := middlewares["app-x-stripprefix"]; !exists {
		t.Error("middleware 'app-x-stripprefix' no encontrado")
	}

	// services
	services, ok := http["services"].(map[string]interface{})
	if !ok {
		t.Fatal("clave 'services' ausente")
	}
	if _, exists := services["app-x"]; !exists {
		t.Error("service 'app-x' no encontrado")
	}
}

func TestSync_VariasRoutes(t *testing.T) {
	mgr, configPath := nuevoManager(t)

	routes := []proxy.Route{
		{AppName: "api",      TargetPort: 9001},
		{AppName: "frontend", TargetPort: 9002},
		{AppName: "worker",   TargetPort: 9003},
	}
	if err := mgr.Sync(context.Background(), routes); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := string(mustReadFile(t, configPath))

	for _, r := range routes {
		if !strings.Contains(content, r.AppName+":") {
			t.Errorf("app %q no encontrada en el YAML", r.AppName)
		}
		portStr := fmt.Sprintf("http://localhost:%d", r.TargetPort)
		if !strings.Contains(content, portStr) {
			t.Errorf("puerto %d de %q no encontrado en el YAML", r.TargetPort, r.AppName)
		}
	}
}

func TestSync_DirectorioInexistente(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "no-existe", "dynamic.yml")
	mgr := proxy.NewTraefikFileProxyManager(configPath)

	err := mgr.Sync(context.Background(), []proxy.Route{{AppName: "x", TargetPort: 1234}})
	if err == nil {
		t.Fatal("se esperaba error con directorio inexistente, got nil")
	}
}

func TestSync_SobreescribeContenidoPrevio(t *testing.T) {
	mgr, configPath := nuevoManager(t)

	// primera sincronización con dos apps
	if err := mgr.Sync(context.Background(), []proxy.Route{
		{AppName: "app-a", TargetPort: 9010},
		{AppName: "app-b", TargetPort: 9011},
	}); err != nil {
		t.Fatalf("primera Sync: %v", err)
	}

	// segunda sincronización solo con app-a (app-b fue eliminada)
	if err := mgr.Sync(context.Background(), []proxy.Route{
		{AppName: "app-a", TargetPort: 9010},
	}); err != nil {
		t.Fatalf("segunda Sync: %v", err)
	}

	content := string(mustReadFile(t, configPath))
	if strings.Contains(content, "app-b") {
		t.Error("app-b debería haber sido eliminada de la configuración")
	}
	if !strings.Contains(content, "app-a") {
		t.Error("app-a debería seguir presente")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("mustReadFile(%q): %v", path, err)
	}
	return data
}
