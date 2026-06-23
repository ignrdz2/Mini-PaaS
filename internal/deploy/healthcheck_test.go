package deploy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ignrdz2/mini-paas/internal/deploy"
)

func TestWaitHealthy_ServidorSano(t *testing.T) {
	// servidor que responde 200 inmediatamente
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// extraer el puerto del servidor de prueba
	port := puertoDeURL(t, srv.URL)

	err := deploy.WaitHealthy(context.Background(), "localhost", port, "/", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitHealthy debería pasar con servidor sano, got: %v", err)
	}
}

func TestWaitHealthy_HealthPath(t *testing.T) {
	// servidor que solo considera sano el path /health
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	port := puertoDeURL(t, srv.URL)

	// 404 es < 500, así que también es healthy
	err := deploy.WaitHealthy(context.Background(), "localhost", port, "/health", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitHealthy con /health: %v", err)
	}
}

func TestWaitHealthy_Timeout(t *testing.T) {
	// encontrar un puerto libre que nadie esté escuchando
	port := puertoCerrado(t)

	inicio := time.Now()
	err := deploy.WaitHealthy(context.Background(), "localhost", port, "/", 1*time.Second)
	elapsed := time.Since(inicio)

	if err == nil {
		t.Fatal("se esperaba error por timeout, got nil")
	}
	// debe haber tardado alrededor de 1s (±500ms de margen)
	if elapsed > 2*time.Second {
		t.Errorf("el timeout tardó demasiado: %s", elapsed)
	}
}

func TestWaitHealthy_ContextCancelado(t *testing.T) {
	port := puertoCerrado(t)

	ctx, cancel := context.WithCancel(context.Background())
	// cancelar inmediatamente
	cancel()

	err := deploy.WaitHealthy(ctx, "localhost", port, "/", 30*time.Second)
	if err == nil {
		t.Fatal("se esperaba error con contexto cancelado, got nil")
	}
}

// puertoDeURL extrae el puerto numérico de una URL del tipo http://127.0.0.1:<port>.
func puertoDeURL(t *testing.T, rawURL string) int {
	t.Helper()
	// rawURL tiene forma http://127.0.0.1:PORT
	parts := strings.Split(rawURL, ":")
	portStr := parts[len(parts)-1]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("no se pudo parsear puerto de %q: %v", rawURL, err)
	}
	return port
}

// puertoCerrado retorna un puerto libre (nadie escuchando) para simular un servidor caído.
func puertoCerrado(t *testing.T) int {
	t.Helper()
	// usar httptest para conseguir un puerto libre, luego cerrar el servidor
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	port := puertoDeURL(t, srv.URL)
	srv.Close() // cerrar inmediatamente — el puerto queda libre
	return port
}
