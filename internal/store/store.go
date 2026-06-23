package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// Store define las operaciones de persistencia del sistema.
type Store interface {
	CreateApp(ctx context.Context, name, repoURL, healthPath string) (App, error)
	GetApp(ctx context.Context, name string) (App, error)
	ListApps(ctx context.Context) ([]App, error)
	DeleteApp(ctx context.Context, name string) error

	CreateDeployment(ctx context.Context, appID pgtype.UUID, imageTag string) (Deployment, error)
	GetDeployment(ctx context.Context, id pgtype.UUID) (Deployment, error)
	ListDeployments(ctx context.Context, appID pgtype.UUID) ([]Deployment, error)
	GetActiveDeployment(ctx context.Context, appID pgtype.UUID) (Deployment, error)
	UpdateDeploymentStatus(ctx context.Context, params UpdateDeploymentParams) (Deployment, error)
}

// UpdateDeploymentParams envuelve el ID del deployment y los campos modificables.
// Los campos opcionales usan pgtype.* donde Valid=false representa NULL (sin cambio).
type UpdateDeploymentParams struct {
	ID           pgtype.UUID
	Status       string
	ContainerID  pgtype.Text
	InternalPort pgtype.Int4
	FinishedAt   pgtype.Timestamptz
	ErrorMessage pgtype.Text
}
