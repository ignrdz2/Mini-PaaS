package docker

import (
	"context"
	"fmt"
	"net"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// DockerClient envuelve el SDK oficial de Docker para las operaciones de runtime del orquestador.
type DockerClient struct {
	cli *client.Client
}

// NewDockerClient crea un DockerClient respetando la variable de entorno DOCKER_HOST.
func NewDockerClient() (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("error al crear cliente Docker: %w", err)
	}
	return &DockerClient{cli: cli}, nil
}

// Close libera los recursos del cliente.
func (d *DockerClient) Close() error {
	return d.cli.Close()
}

// RunContainer crea y arranca un container a partir de imageTag.
// Convención de puerto: el orquestador elige un puerto libre en el host y lo inyecta al
// container como variable de entorno PORT. La app deployada es responsable de leer PORT
// y escuchar en ese puerto. No se hace port binding explícito en Docker — el healthcheck
// y el proxy se comunican directamente con el container vía red host.
func (d *DockerClient) RunContainer(ctx context.Context, imageTag, appName string) (string, int, error) {
	port, err := FindFreePort()
	if err != nil {
		return "", 0, fmt.Errorf("no se encontró puerto libre: %w", err)
	}

	cfg := &container.Config{
		Image: imageTag,
		Env:   []string{fmt.Sprintf("PORT=%d", port)},
		Labels: map[string]string{
			"mini-paas.app": appName,
		},
	}
	hostCfg := &container.HostConfig{
		NetworkMode: "host",
	}

	resp, err := d.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return "", 0, fmt.Errorf("error al crear container para %q: %w", appName, err)
	}

	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", 0, fmt.Errorf("error al iniciar container %s: %w", resp.ID[:12], err)
	}

	return resp.ID, port, nil
}

// StopAndRemoveContainer detiene y elimina el container con el ID dado.
// Es tolerante a que el container ya no exista — no retorna error en ese caso.
func (d *DockerClient) StopAndRemoveContainer(ctx context.Context, containerID string) error {
	// intentar detener; ignorar error si ya no existe
	if err := d.cli.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		if !client.IsErrNotFound(err) {
			return fmt.Errorf("error al detener container %s: %w", containerID[:12], err)
		}
	}

	if err := d.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		if !client.IsErrNotFound(err) {
			return fmt.Errorf("error al remover container %s: %w", containerID[:12], err)
		}
	}

	return nil
}

// FindFreePort obtiene un puerto TCP libre pidiendo al OS que asigne uno en :0.
func FindFreePort() (int, error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("no se pudo abrir listener para encontrar puerto libre: %w", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
