package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

type WorkspaceService struct {
	db port.Datastore
}

func NewWorkspaceService(db port.Datastore) *WorkspaceService {
	return &WorkspaceService{db: db}
}

func (s *WorkspaceService) CreateWorkspace(ctx context.Context, name, description string) (*domain.Workspace, error) {
	ws := &domain.Workspace{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.db.CreateWorkspace(ctx, ws); err != nil {
		slog.ErrorContext(ctx, "create workspace", "name", name, "error", err)
		return nil, mapErr(err)
	}
	return ws, nil
}

func (s *WorkspaceService) GetWorkspace(ctx context.Context, name string) (*domain.Workspace, error) {
	ws, err := s.db.GetWorkspace(ctx, name)
	if err != nil {
		slog.ErrorContext(ctx, "get workspace", "name", name, "error", err)
	}
	return ws, mapErr(err)
}

func (s *WorkspaceService) ListWorkspaces(ctx context.Context, limit, offset int) ([]*domain.Workspace, error) {
	workspaces, err := s.db.ListWorkspaces(ctx, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "list workspaces", "error", err)
		return nil, err
	}
	if workspaces == nil {
		workspaces = []*domain.Workspace{}
	}
	return workspaces, nil
}

func (s *WorkspaceService) DeleteWorkspace(ctx context.Context, name string) error {
	if err := s.db.DeleteWorkspace(ctx, name); err != nil {
		slog.ErrorContext(ctx, "delete workspace", "name", name, "error", err)
		return mapErr(err)
	}
	return nil
}
