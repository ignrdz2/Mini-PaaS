package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implementa Store contra una base de datos PostgreSQL usando pgxpool.
type PostgresStore struct {
	*Queries
	pool *pgxpool.Pool
}

// garantía en tiempo de compilación de que PostgresStore satisface Store.
var _ Store = (*PostgresStore)(nil)

// NewPostgresStore abre un pool de conexiones y verifica conectividad con Ping.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresStore{
		Queries: New(pool),
		pool:    pool,
	}, nil
}

// Close libera las conexiones del pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) CreateApp(ctx context.Context, name, repoURL, healthPath string) (App, error) {
	return s.Queries.CreateApp(ctx, CreateAppParams{
		Name:       name,
		RepoUrl:    repoURL,
		HealthPath: healthPath,
	})
}

func (s *PostgresStore) GetApp(ctx context.Context, name string) (App, error) {
	return s.Queries.GetAppByName(ctx, name)
}

// ListApps es satisfecha por el método promovido de *Queries (misma firma).
// DeleteApp es satisfecha por el método promovido de *Queries (misma firma).
// GetDeployment es satisfecha por el método promovido de *Queries (misma firma).

func (s *PostgresStore) CreateDeployment(ctx context.Context, appID pgtype.UUID, imageTag string) (Deployment, error) {
	return s.Queries.CreateDeployment(ctx, CreateDeploymentParams{
		AppID:    appID,
		ImageTag: imageTag,
	})
}

func (s *PostgresStore) ListDeployments(ctx context.Context, appID pgtype.UUID) ([]Deployment, error) {
	return s.Queries.ListDeploymentsByApp(ctx, appID)
}

func (s *PostgresStore) GetActiveDeployment(ctx context.Context, appID pgtype.UUID) (Deployment, error) {
	return s.Queries.GetActiveDeploymentByApp(ctx, appID)
}

// UpdateDeploymentStatus convierte UpdateDeploymentParams al tipo generado por sqlc.
// Ambos structs tienen campos idénticos, por lo que la conversión directa es válida en Go.
func (s *PostgresStore) UpdateDeploymentStatus(ctx context.Context, params UpdateDeploymentParams) (Deployment, error) {
	return s.Queries.UpdateDeploymentStatus(ctx, UpdateDeploymentStatusParams(params))
}

func (s *PostgresStore) CreateDeploymentLog(ctx context.Context, deploymentID pgtype.UUID, message string) error {
	_, err := s.Queries.CreateDeploymentLog(ctx, CreateDeploymentLogParams{
		DeploymentID: deploymentID,
		Message:      message,
	})
	return err
}

func (s *PostgresStore) ListDeploymentLogs(ctx context.Context, deploymentID pgtype.UUID) ([]DeploymentLog, error) {
	return s.Queries.ListDeploymentLogs(ctx, deploymentID)
}

func (s *PostgresStore) ListDeploymentLogsAfter(ctx context.Context, deploymentID pgtype.UUID, afterID int64) ([]DeploymentLog, error) {
	return s.Queries.ListDeploymentLogsAfter(ctx, ListDeploymentLogsAfterParams{
		DeploymentID: deploymentID,
		AfterID:      afterID,
	})
}
