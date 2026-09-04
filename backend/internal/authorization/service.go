package authorization

import (
	"context"
	"errors"

	"github.com/altasci/network-storage/backend/internal/repository"
)

type Service struct{ store *repository.Store }

func New(store *repository.Store) *Service { return &Service{store: store} }

func (s *Service) CanReadProject(ctx context.Context, u repository.User, projectID string) error {
	if u.Status != "active" {
		return repository.ErrForbidden
	}
	if u.Role == "admin" {
		_, err := s.store.ProjectByID(ctx, projectID)
		return err
	}
	_, err := s.store.ProjectPermission(ctx, projectID, u.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.ErrForbidden
	}
	return err
}
func (s *Service) CanWriteProject(ctx context.Context, u repository.User, projectID string) error {
	if u.Status != "active" {
		return repository.ErrForbidden
	}
	if u.Role == "admin" {
		_, err := s.store.ProjectByID(ctx, projectID)
		return err
	}
	if !u.WriteEnabled {
		return repository.ErrForbidden
	}
	p, err := s.store.ProjectPermission(ctx, projectID, u.ID)
	if err != nil || p != "write" {
		return repository.ErrForbidden
	}
	return nil
}
func (s *Service) CanCreateProject(u repository.User) error {
	if u.Status == "active" && (u.Role == "admin" || u.WriteEnabled) {
		return nil
	}
	return repository.ErrForbidden
}
func (s *Service) CanManageProject(u repository.User) error {
	if u.Status == "active" && u.Role == "admin" {
		return nil
	}
	return repository.ErrForbidden
}
func (s *Service) CanManageUsers(u repository.User) error { return s.CanManageProject(u) }
func (s *Service) CanShareNode(ctx context.Context, u repository.User, node repository.Node) error {
	return s.CanWriteProject(ctx, u, node.ProjectID)
}
