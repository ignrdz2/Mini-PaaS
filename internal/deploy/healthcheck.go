package deploy

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// WaitHealthy hace polling al endpoint http://<host>:<port><healthPath> hasta que responde
// con status < 500, o hasta que se agota el timeout o se cancela el contexto.
// El parámetro host permite usar "localhost" (proceso local) o "host.docker.internal"
// (cuando el orquestador corre dentro de Docker Desktop en Windows/Mac).
func WaitHealthy(ctx context.Context, host string, port int, healthPath string, timeout time.Duration) error {
	url := fmt.Sprintf("http://%s:%d%s", host, port, healthPath)

	// cliente con timeout corto por request para no quedar bloqueado en un intento individual
	httpClient := &http.Client{Timeout: 2 * time.Second}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("healthcheck agotó el tiempo de espera (%s) para %s: %w", timeout, url, ctx.Err())
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				// error de construcción del request, muy improbable
				continue
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				// container todavía no está escuchando — reintentar en el próximo tick
				continue
			}
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
	}
}
