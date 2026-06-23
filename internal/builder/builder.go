package builder

import "context"

// Builder convierte el código fuente de un repo ya clonado en una imagen Docker.
type Builder interface {
	Build(ctx context.Context, sourcePath string, imageTag string) (BuildResult, error)
}

// BuildResult contiene el resultado de un build exitoso.
// Logs incluye los últimos 4000 caracteres del output del build (útil para debugging).
type BuildResult struct {
	ImageTag string
	Logs     string
}
