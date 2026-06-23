package proxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// TraefikFileProxyManager implementa ProxyManager escribiendo la configuración dinámica
// de Traefik como archivo YAML (file provider). La escritura es atómica vía os.Rename.
type TraefikFileProxyManager struct {
	configPath string // path al archivo YAML dinámico de Traefik
}

// garantía en tiempo de compilación de que TraefikFileProxyManager satisface ProxyManager.
var _ ProxyManager = (*TraefikFileProxyManager)(nil)

// NewTraefikFileProxyManager crea un manager apuntando a configPath.
// configPath debe ser el path completo al archivo YAML, ej. "traefik/dynamic/dynamic.yml".
func NewTraefikFileProxyManager(configPath string) *TraefikFileProxyManager {
	return &TraefikFileProxyManager{configPath: configPath}
}

// --- estructuras internas para serialización YAML ---

type traefikConfig struct {
	HTTP *traefikHTTP `yaml:"http,omitempty"`
}

type traefikHTTP struct {
	Routers     map[string]*traefikRouter     `yaml:"routers,omitempty"`
	Middlewares map[string]*traefikMiddleware `yaml:"middlewares,omitempty"`
	Services    map[string]*traefikService    `yaml:"services,omitempty"`
}

type traefikRouter struct {
	Rule        string   `yaml:"rule"`
	Service     string   `yaml:"service"`
	Middlewares []string `yaml:"middlewares"`
}

type traefikMiddleware struct {
	StripPrefix *traefikStripPrefix `yaml:"stripPrefix,omitempty"`
}

type traefikStripPrefix struct {
	Prefixes []string `yaml:"prefixes"`
}

type traefikService struct {
	LoadBalancer *traefikLoadBalancer `yaml:"loadBalancer"`
}

type traefikLoadBalancer struct {
	Servers []traefikServer `yaml:"servers"`
}

type traefikServer struct {
	URL string `yaml:"url"`
}

// Sync regenera el archivo de configuración de Traefik con el conjunto completo de routes.
// Si routes está vacío escribe un YAML vacío válido. La escritura es atómica.
func (t *TraefikFileProxyManager) Sync(_ context.Context, routes []Route) error {
	var cfg traefikConfig

	if len(routes) > 0 {
		http := &traefikHTTP{
			Routers:     make(map[string]*traefikRouter, len(routes)),
			Middlewares: make(map[string]*traefikMiddleware, len(routes)),
			Services:    make(map[string]*traefikService, len(routes)),
		}

		for _, r := range routes {
			middlewareName := r.AppName + "-stripprefix"

			http.Routers[r.AppName] = &traefikRouter{
				Rule:        fmt.Sprintf("PathPrefix(`/%s`)", r.AppName),
				Service:     r.AppName,
				Middlewares: []string{middlewareName},
			}
			http.Middlewares[middlewareName] = &traefikMiddleware{
				StripPrefix: &traefikStripPrefix{
					Prefixes: []string{"/" + r.AppName},
				},
			}
			http.Services[r.AppName] = &traefikService{
				LoadBalancer: &traefikLoadBalancer{
					Servers: []traefikServer{
						{URL: fmt.Sprintf("http://localhost:%d", r.TargetPort)},
					},
				},
			}
		}

		cfg.HTTP = http
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("error al serializar configuración de Traefik: %w", err)
	}

	return escribirAtomico(t.configPath, data)
}

// escribirAtomico escribe data a un archivo temporal en el mismo directorio que dst,
// luego hace os.Rename para que la sustitución sea atómica desde la perspectiva de Traefik.
func escribirAtomico(dst string, data []byte) error {
	dir := filepath.Dir(dst)

	tmp, err := os.CreateTemp(dir, ".traefik-tmp-*")
	if err != nil {
		return fmt.Errorf("error al crear archivo temporal en %q: %w", dir, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("error al escribir archivo temporal: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("error al cerrar archivo temporal: %w", err)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("error al reemplazar %q con configuración nueva: %w", dst, err)
	}

	return nil
}
