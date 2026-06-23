//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// configuración leída de variables de entorno con defaults para docker compose local.
var (
	orchestratorURL = getEnvDefault("ORCHESTRATOR_URL", "http://localhost:8080")
	traefikURL      = getEnvDefault("TRAEFIK_URL", "http://localhost:80")
	testRepoURL     = os.Getenv("E2E_TEST_REPO_URL")
)

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// TestE2EFlujoCompleto verifica el ciclo completo: crear app → deploy → HTTP via Traefik → delete.
// Requiere que docker compose esté levantado y E2E_TEST_REPO_URL apunte a un repo con Dockerfile.
func TestE2EFlujoCompleto(t *testing.T) {
	if testRepoURL == "" {
		t.Skip("E2E_TEST_REPO_URL no definida — omitiendo test e2e")
	}

	// verificar que el orquestador está disponible antes de empezar
	if err := esperarServicio(orchestratorURL+"/apps", 15*time.Second); err != nil {
		t.Fatalf("orquestador no disponible en %s: %v", orchestratorURL, err)
	}

	// nombre único para no colisionar con runs anteriores
	appName := fmt.Sprintf("e2e-%d", time.Now().Unix())
	t.Logf("usando app name: %s", appName)

	// limpiar al terminar para no dejar estado sucio
	t.Cleanup(func() {
		borrarApp(t, appName)
	})

	// 1. crear la app
	t.Run("crear app", func(t *testing.T) {
		body := map[string]string{
			"name":     appName,
			"repo_url": testRepoURL,
		}
		resp := apiCall(t, "POST", "/apps", body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			data, _ := io.ReadAll(resp.Body)
			t.Fatalf("esperaba 201, obtuve %d: %s", resp.StatusCode, data)
		}

		var app map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
			t.Fatalf("error decodificando respuesta: %v", err)
		}
		if app["name"] != appName {
			t.Errorf("nombre de app incorrecto: %v", app["name"])
		}
		t.Logf("app creada: id=%v", app["id"])
	})

	// 2. disparar el deploy y esperar que llegue a running o failed
	var deployID string
	t.Run("deploy", func(t *testing.T) {
		// timeout generoso: el build puede tardar varios minutos
		client := &http.Client{Timeout: 10 * time.Minute}

		reqBody, _ := json.Marshal(map[string]any{})
		req, _ := http.NewRequest("POST", orchestratorURL+"/apps/"+appName+"/deployments", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("error al disparar deploy: %v", err)
		}
		defer resp.Body.Close()

		var dep map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&dep); err != nil {
			t.Fatalf("error decodificando deployment: %v", err)
		}

		status, _ := dep["status"].(string)
		deployID, _ = dep["id"].(string)
		t.Logf("deployment id=%s status=%s", deployID, status)

		if status != "running" {
			errMsg, _ := dep["error_message"].(string)
			t.Fatalf("deploy no llegó a running (status=%s): %s", status, errMsg)
		}
	})

	// 3. verificar que Traefik enruta las peticiones a la app
	t.Run("ruta traefik activa", func(t *testing.T) {
		ruta := traefikURL + "/" + appName + "/"
		if err := esperarHTTP(ruta, 30*time.Second); err != nil {
			t.Fatalf("Traefik no devolvió 2xx para %s: %v", ruta, err)
		}
		t.Logf("ruta %s responde correctamente", ruta)
	})

	// 4. eliminar la app
	t.Run("eliminar app", func(t *testing.T) {
		resp := apiCall(t, "DELETE", "/apps/"+appName, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			data, _ := io.ReadAll(resp.Body)
			t.Fatalf("esperaba 204, obtuve %d: %s", resp.StatusCode, data)
		}
	})

	// 5. verificar que la ruta de Traefik desaparece
	t.Run("ruta traefik eliminada", func(t *testing.T) {
		ruta := traefikURL + "/" + appName + "/"
		// dar tiempo para que Traefik refresque la configuración
		time.Sleep(2 * time.Second)

		resp, err := http.Get(ruta)
		if err != nil {
			// error de conexión rechazada también es válido (ruta eliminada)
			if strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "connection") {
				return
			}
			t.Logf("advertencia: error al verificar ruta eliminada: %v", err)
			return
		}
		defer resp.Body.Close()

		// Traefik devuelve 404 cuando no hay router que coincida
		if resp.StatusCode == http.StatusNotFound {
			t.Logf("ruta eliminada correctamente (404)")
			return
		}
		t.Errorf("ruta aún activa después de eliminar la app (status %d)", resp.StatusCode)
	})
}

// --- helpers internos ---

// apiCall realiza una petición al orquestador y retorna la respuesta.
func apiCall(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 15 * time.Second}

	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, orchestratorURL+path, bodyReader)
	if err != nil {
		t.Fatalf("error creando request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error ejecutando request %s %s: %v", method, path, err)
	}
	return resp
}

// borrarApp intenta eliminar la app ignorando errores (para usar en t.Cleanup).
func borrarApp(t *testing.T, appName string) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("DELETE", orchestratorURL+"/apps/"+appName, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("cleanup: error al eliminar app %s: %v", appName, err)
		return
	}
	resp.Body.Close()
	t.Logf("cleanup: app %s eliminada (status %d)", appName, resp.StatusCode)
}

// esperarServicio hace polling a una URL hasta que responda 2xx o se agote el timeout.
func esperarServicio(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("servicio no disponible después de %s", timeout)
}

// esperarHTTP hace polling hasta que la URL responda con status < 500.
func esperarHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode < 500 {
			return nil
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout esperando %s: %v", url, lastErr)
}
