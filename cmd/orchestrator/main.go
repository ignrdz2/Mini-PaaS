package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/ignrdz2/mini-paas/internal/api"
	"github.com/ignrdz2/mini-paas/internal/builder"
	"github.com/ignrdz2/mini-paas/internal/deploy"
	"github.com/ignrdz2/mini-paas/internal/docker"
	"github.com/ignrdz2/mini-paas/internal/proxy"
	"github.com/ignrdz2/mini-paas/internal/store"
)

func main() {
	// leer configuración desde variables de entorno con valores por defecto para desarrollo local
	databaseURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/mini_paas?sslmode=disable")
	traefikConfigPath := getEnv("TRAEFIK_CONFIG_PATH", "/tmp/traefik-dynamic.yml")
	listenAddr := getEnv("LISTEN_ADDR", ":8080")
	// HEALTH_HOST controla el host usado en el healthcheck de cada deploy.
	// Usar "host.docker.internal" cuando el orquestador corre dentro de Docker Desktop (Windows/Mac).
	healthHost := getEnv("HEALTH_HOST", "localhost")
	// PROXY_TARGET_HOST controla el host que Traefik usa para rutear tráfico a las apps.
	// Usar "host.docker.internal" en Docker Desktop (Windows/Mac).
	proxyTargetHost := getEnv("PROXY_TARGET_HOST", "localhost")

	ctx := context.Background()

	// conectar al store de Postgres
	s, err := store.NewPostgresStore(ctx, databaseURL)
	if err != nil {
		log.Fatalf("error al conectar con Postgres: %v", err)
	}
	defer s.Close()

	// aplicar migraciones al arrancar (idempotente)
	if err := s.RunMigrations(ctx); err != nil {
		log.Fatalf("error al aplicar migraciones: %v", err)
	}
	log.Println("migraciones aplicadas correctamente")

	// inicializar el cliente de Docker
	dockerClient, err := docker.NewDockerClient()
	if err != nil {
		log.Fatalf("error al crear el cliente de Docker: %v", err)
	}

	// inicializar el proxy manager
	proxyManager := proxy.NewTraefikFileProxyManager(traefikConfigPath, proxyTargetHost)

	// inicializar el builder
	b := builder.NewDockerfileBuilder(dockerClient.Client())

	// inicializar el orquestador
	orch := deploy.NewOrchestrator(s, b, dockerClient, proxyManager, deploy.WithHealthHost(healthHost))

	// crear y arrancar el servidor HTTP
	srv := api.NewServer(s, orch, dockerClient, proxyManager)

	log.Printf("servidor escuchando en %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, srv); err != nil {
		log.Fatalf("error en el servidor HTTP: %v", err)
	}
}

// getEnv retorna el valor de la variable de entorno o el valor por defecto si no está definida.
func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
