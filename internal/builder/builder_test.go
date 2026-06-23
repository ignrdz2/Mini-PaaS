package builder_test

import (
	"context"
	"os"
	"strings"
	"testing"

	dockerimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/ignrdz2/mini-paas/internal/builder"
)

// nuevoCliente crea un cliente Docker usando las variables de entorno del sistema.
func nuevoCliente(t *testing.T) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("no se pudo crear cliente Docker: %v", err)
	}
	t.Cleanup(func() { cli.Close() })
	return cli
}

func TestBuild_HappyPath(t *testing.T) {
	cli := nuevoCliente(t)
	b := builder.NewDockerfileBuilder(cli)

	// directorio temporal con un Dockerfile mínimo válido
	dir := t.TempDir()
	dockerfile := "FROM alpine:3.20\nCMD [\"echo\", \"ok\"]\n"
	if err := os.WriteFile(dir+"/Dockerfile", []byte(dockerfile), 0644); err != nil {
		t.Fatalf("error escribiendo Dockerfile: %v", err)
	}

	imageTag := "mini-paas-test-happypath:latest"
	ctx := context.Background()

	result, err := b.Build(ctx, dir, imageTag)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.ImageTag != imageTag {
		t.Errorf("ImageTag esperado %q, got %q", imageTag, result.ImageTag)
	}
	if result.Logs == "" {
		t.Error("Logs no debería estar vacío en un build exitoso")
	}

	// limpiar imagen generada al terminar
	t.Cleanup(func() {
		cli.ImageRemove(ctx, imageTag, dockerimage.RemoveOptions{}) //nolint
	})
}

func TestBuild_SinDockerfile(t *testing.T) {
	cli := nuevoCliente(t)
	b := builder.NewDockerfileBuilder(cli)

	// directorio vacío — sin Dockerfile
	dir := t.TempDir()

	_, err := b.Build(context.Background(), dir, "mini-paas-test-nodockerfile:latest")
	if err == nil {
		t.Fatal("se esperaba error por Dockerfile ausente, got nil")
	}
	if !strings.Contains(err.Error(), "Dockerfile") {
		t.Errorf("el error debería mencionar Dockerfile, got: %v", err)
	}
}

func TestBuild_DockerfileInvalido(t *testing.T) {
	cli := nuevoCliente(t)
	b := builder.NewDockerfileBuilder(cli)

	dir := t.TempDir()
	// instrucción inexistente → docker build falla
	dockerfile := "FROM alpine:3.20\nINSTRUCCION_INVALIDA esto_no_existe\n"
	if err := os.WriteFile(dir+"/Dockerfile", []byte(dockerfile), 0644); err != nil {
		t.Fatalf("error escribiendo Dockerfile: %v", err)
	}

	_, err := b.Build(context.Background(), dir, "mini-paas-test-invalid:latest")
	if err == nil {
		t.Fatal("se esperaba error por Dockerfile inválido, got nil")
	}
	// el mensaje de error debe indicar algún fallo de build (parse error, stream error, etc.)
	if !strings.Contains(err.Error(), "build") && !strings.Contains(err.Error(), "Dockerfile") {
		t.Errorf("el error debería mencionar el fallo de build, got: %v", err)
	}
}
