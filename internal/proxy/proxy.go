package proxy

import "context"

// ProxyManager sincroniza la configuración del reverse proxy con el estado actual del sistema.
type ProxyManager interface {
	Sync(ctx context.Context, routes []Route) error
}

// Route representa una app activa con el puerto host de su container.
type Route struct {
	AppName    string
	TargetPort int // puerto host del container
}
