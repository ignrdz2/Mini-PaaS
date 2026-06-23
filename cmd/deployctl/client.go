package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// cliente HTTP que se comunica con el orquestador.
type apiClient struct {
	baseURL    string
	httpClient *http.Client
}

// newAPIClient crea un cliente usando DEPLOYCTL_API_URL o el default local.
func newAPIClient(timeout time.Duration) *apiClient {
	base := os.Getenv("DEPLOYCTL_API_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	return &apiClient{
		baseURL:    base,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// newAPIClientSinTimeout crea un cliente sin timeout (para apps deploy).
func newAPIClientSinTimeout() *apiClient {
	base := os.Getenv("DEPLOYCTL_API_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	return &apiClient{
		baseURL:    base,
		httpClient: &http.Client{},
	}
}

// hacer ejecuta una petición HTTP y decodifica la respuesta JSON en dest.
// Si la respuesta contiene {"error":"..."}, retorna un error con ese mensaje.
func (c *apiClient) hacer(ctx context.Context, method, path string, body any, dest any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("error al serializar body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("error al crear request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error al conectar con el orquestador: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error al leer respuesta: %w", err)
	}

	// intentar detectar respuesta de error del servidor antes de decodificar
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("error HTTP %d", resp.StatusCode)
	}

	// para 204 No Content no hay body que decodificar
	if resp.StatusCode == http.StatusNoContent || dest == nil {
		return nil
	}

	if err := json.Unmarshal(respBody, dest); err != nil {
		return fmt.Errorf("error al decodificar respuesta: %w", err)
	}
	return nil
}

// --- tipos que reflejan las respuestas JSON del orquestador ---

type appResp struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	RepoURL          string          `json:"repo_url"`
	HealthPath       string          `json:"health_path"`
	CreatedAt        string          `json:"created_at"`
	ActiveDeployment *deploymentResp `json:"active_deployment"`
}

type deploymentResp struct {
	ID           string  `json:"id"`
	AppID        string  `json:"app_id"`
	ImageTag     string  `json:"image_tag"`
	Status       string  `json:"status"`
	ContainerID  *string `json:"container_id"`
	InternalPort *int32  `json:"internal_port"`
	CreatedAt    string  `json:"created_at"`
	FinishedAt   *string `json:"finished_at"`
	ErrorMessage *string `json:"error_message"`
}
