package repository

import (
	"context"
	"database/sql"
)

const projectColumns = `p.id,p.name,p.description,p.storage_backend_id,p.created_by,p.status,p.created_at,p.updated_at`

func scanProject(scanner interface{ Scan(...any) error }) (Project, error) {
	var p Project
	err := scanner.Scan(&p.ID, &p.Name, &p.Description, &p.StorageBackendID, &p.CreatedBy, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	return p, mapError(err)
}

func (s *Store) CreateProject(ctx context.Context, p Project, creatorIsAdmin bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT enabled FROM storage_backends WHERE id=?`, p.StorageBackendID).Scan(&enabled); err != nil {
		return mapError(err)
	}
	if !enabled {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,description,storage_backend_id,created_by,status,created_at,updated_at) VALUES(?,?,?,?,?,'active',?,?)`, p.ID, p.Name, p.Description, p.StorageBackendID, p.CreatedBy, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return mapError(err)
	}
	if !creatorIsAdmin {
		_, err = tx.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,permission,granted_by,created_at,updated_at) VALUES(?,?,'write',?,?,?)`, p.ID, p.CreatedBy, p.CreatedBy, p.CreatedAt, p.UpdatedAt)
		if err != nil {
			return mapError(err)
		}
	}
	return tx.Commit()
}
func (s *Store) ProjectByID(ctx context.Context, id string) (Project, error) {
	return scanProject(s.DB.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects p WHERE p.id=? AND p.status='active'`, id))
}
func (s *Store) ListProjects(ctx context.Context, user User) ([]Project, error) {
	query := `SELECT ` + projectColumns + `,'' FROM projects p WHERE p.status='active' ORDER BY p.created_at`
	args := []any{}
	if user.Role != "admin" {
		query = `SELECT ` + projectColumns + `,pm.permission FROM projects p JOIN project_members pm ON pm.project_id=p.id WHERE p.status='active' AND pm.user_id=? ORDER BY p.created_at`
		args = append(args, user.ID)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.StorageBackendID, &p.CreatedBy, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.Permission); err != nil {
			return nil, err
		}
		if user.Role == "admin" {
			p.Permission = "admin"
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) UpdateProject(ctx context.Context, id, name, description string) (Project, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE projects SET name=?,description=?,updated_at=? WHERE id=? AND status='active'`, name, description, NowMS(), id)
	if err != nil {
		return Project{}, mapError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Project{}, ErrNotFound
	}
	return s.ProjectByID(ctx, id)
}
func (s *Store) ProjectPermission(ctx context.Context, projectID, userID string) (string, error) {
	var p string
	err := s.DB.QueryRowContext(ctx, `SELECT pm.permission FROM project_members pm JOIN projects p ON p.id=pm.project_id WHERE pm.project_id=? AND pm.user_id=? AND p.status='active'`, projectID, userID).Scan(&p)
	return p, mapError(err)
}
func (s *Store) ListMembers(ctx context.Context, projectID string) ([]ProjectMember, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT project_id,user_id,permission,granted_by,created_at,updated_at FROM project_members WHERE project_id=? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectMember
	for rows.Next() {
		var m ProjectMember
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Permission, &m.GrantedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) PutMember(ctx context.Context, m ProjectMember) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id,permission,granted_by,created_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(project_id,user_id) DO UPDATE SET permission=excluded.permission,granted_by=excluded.granted_by,updated_at=excluded.updated_at`, m.ProjectID, m.UserID, m.Permission, m.GrantedBy, m.CreatedAt, m.UpdatedAt)
	return mapError(err)
}
func (s *Store) DeleteMember(ctx context.Context, projectID, userID string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM project_members WHERE project_id=? AND user_id=?`, projectID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkProjectDeleting(ctx context.Context, projectID string) error {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE projects SET status='deleting',updated_at=? WHERE id=? AND status='active'`, now, projectID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM file_blobs WHERE project_id=? AND status IN ('active','pending')`, projectID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fs_nodes SET deleted_at=?,updated_at=? WHERE project_id=? AND deleted_at IS NULL`, now, now, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file_blobs SET status='deleting',deleted_at=? WHERE project_id=? AND status IN ('active','pending')`, now, projectID); err != nil {
		return err
	}
	for _, id := range ids {
		if err := insertJob(ctx, tx, "delete_blob", map[string]string{"blob_id": id}, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
