package docker_test

import (
	"context"
	"testing"
	"time"

	"github.com/ignrdz2/mini-paas/internal/docker"
)

func nuevoCliente(t *testing.T) *docker.DockerClient {
	t.Helper()
	cli, err := docker.NewDockerClient()
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	t.Cleanup(func() { cli.Close() })
	return cli
}

func TestFindFreePort(t *testing.T) {
	port, err := docker.FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	if port <= 0 {
		t.Errorf("se esperaba puerto > 0, got %d", port)
	}
}

func TestRunContainer_y_StopAndRemove(t *testing.T) {
	cli := nuevoCliente(t)
	ctx := context.Background()

	// alpine con un sleep largo — no necesita escuchar en ningún puerto
	containerID, port, err := cli.RunContainer(ctx, "alpine:3.20", "test-run")
	if err != nil {
		// si la imagen no está en caché localmente, hacer pull puede tardar; si falla es
		// probable que sea por eso — lo reportamos con contexto
		t.Fatalf("RunContainer: %v", err)
	}
	if containerID == "" {
		t.Error("containerID no debería estar vacío")
	}
	if port <= 0 {
		t.Errorf("port esperado > 0, got %d", port)
	}

	t.Cleanup(func() {
		// limpiar aunque el test falle a mitad de camino
		cli.StopAndRemoveContainer(context.Background(), containerID) //nolint
	})

	// detener y remover correctamente
	if err := cli.StopAndRemoveContainer(ctx, containerID); err != nil {
		t.Fatalf("StopAndRemoveContainer: %v", err)
	}

	// segunda llamada con el mismo ID — debe ser tolerante (container ya no existe)
	if err := cli.StopAndRemoveContainer(ctx, containerID); err != nil {
		t.Errorf("segunda llamada a StopAndRemoveContainer debería ser tolerante, got: %v", err)
	}
}

func TestRunContainer_ImagenInexistente(t *testing.T) {
	cli := nuevoCliente(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, err := cli.RunContainer(ctx, "imagen-que-no-existe-mini-paas:latest", "test-fail")
	if err == nil {
		t.Fatal("se esperaba error con imagen inexistente, got nil")
	}
}
