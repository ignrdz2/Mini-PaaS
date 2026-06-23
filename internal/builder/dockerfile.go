package builder

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

// DockerfileBuilder implementa Builder usando el SDK oficial de Docker.
// Requiere que el directorio sourcePath contenga un Dockerfile en su raíz.
type DockerfileBuilder struct {
	docker *client.Client
}

// garantía en tiempo de compilación de que DockerfileBuilder satisface Builder.
var _ Builder = (*DockerfileBuilder)(nil)

// NewDockerfileBuilder crea un DockerfileBuilder con el cliente Docker dado.
func NewDockerfileBuilder(cli *client.Client) *DockerfileBuilder {
	return &DockerfileBuilder{docker: cli}
}

// Build construye una imagen Docker a partir de sourcePath con el tag imageTag.
// Retorna error si no existe Dockerfile, si el build falla, o si Docker no está disponible.
func (b *DockerfileBuilder) Build(ctx context.Context, sourcePath string, imageTag string) (BuildResult, error) {
	dockerfilePath := filepath.Join(sourcePath, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return BuildResult{}, fmt.Errorf("no se encontró Dockerfile en %s", sourcePath)
	}

	tarBody, err := crearTarDesdeDirectorio(sourcePath)
	if err != nil {
		return BuildResult{}, fmt.Errorf("error al crear tar del contexto de build: %w", err)
	}

	opts := dockertypes.ImageBuildOptions{
		Tags:       []string{imageTag},
		Dockerfile: "Dockerfile",
		Remove:     true,
	}

	resp, err := b.docker.ImageBuild(ctx, tarBody, opts)
	if err != nil {
		return BuildResult{}, fmt.Errorf("error al iniciar docker build: %w", err)
	}
	defer resp.Body.Close()

	logs, buildErr := leerStreamDeBuild(resp.Body)

	if buildErr != nil {
		return BuildResult{}, fmt.Errorf("docker build falló: %w\nlogs:\n%s", buildErr, logs)
	}

	return BuildResult{ImageTag: imageTag, Logs: logs}, nil
}

// mensajeStream representa un mensaje JSON del stream de respuesta de docker build.
type mensajeStream struct {
	Stream      string `json:"stream"`
	Error       string `json:"error"`
	ErrorDetail *struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}

// leerStreamDeBuild consume el stream de build de Docker y retorna los logs acumulados.
// Retorna error si el stream contiene un mensaje de error de Docker.
func leerStreamDeBuild(r io.Reader) (string, error) {
	var buf bytes.Buffer
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Bytes()

		var msg mensajeStream
		if err := json.Unmarshal(line, &msg); err != nil {
			// línea no parseble: agregar como texto plano
			buf.Write(line)
			buf.WriteByte('\n')
			continue
		}

		if msg.Stream != "" {
			buf.WriteString(msg.Stream)
		}

		if msg.Error != "" {
			// acumular el error también en los logs antes de retornar
			buf.WriteString(msg.Error)
			return truncarUltimos4000(buf.String()), fmt.Errorf("%s", msg.Error)
		}
	}

	if err := scanner.Err(); err != nil {
		return truncarUltimos4000(buf.String()), fmt.Errorf("error leyendo stream de build: %w", err)
	}

	return truncarUltimos4000(buf.String()), nil
}

// truncarUltimos4000 retorna los últimos 4000 caracteres del string dado.
func truncarUltimos4000(s string) string {
	const maxLen = 4000
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[len(runes)-maxLen:])
}

// crearTarDesdeDirectorio empaqueta el contenido de dir en un archivo tar en memoria.
func crearTarDesdeDirectorio(dir string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		// usar separadores Unix dentro del tar (Docker espera esto)
		relPath = filepath.ToSlash(relPath)

		info, err := d.Info()
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = relPath

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if !d.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}
