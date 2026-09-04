package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/altasci/network-storage/backend/internal/security"
	"golang.org/x/text/unicode/norm"
)

const nodeColumns = `id,project_id,parent_id,node_type,name,normalized_name,current_blob_id,size,mime_type,created_by,created_at,updated_at,deleted_at`

func scanNode(scanner interface{ Scan(...any) error }) (Node, error) {
	var n Node
	err := scanner.Scan(&n.ID, &n.ProjectID, &n.ParentID, &n.NodeType, &n.Name, &n.NormalizedName, &n.CurrentBlobID, &n.Size, &n.MIMEType, &n.CreatedBy, &n.CreatedAt, &n.UpdatedAt, &n.DeletedAt)
	return n, mapError(err)
}

func NormalizeName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", errors.New("name must be valid UTF-8")
	}
	name = norm.NFC.String(name)
	if len([]byte(name)) < 1 || len([]byte(name)) > 255 {
		return "", errors.New("name must contain 1 to 255 UTF-8 bytes")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", errors.New("name contains a forbidden path component")
	}
	for _, r := range name {
		if r == 0 || r < 0x20 || r == 0x7f {
			return "", errors.New("name contains a control character")
		}
	}
	return name, nil
}

func (s *Store) NodeByID(ctx context.Context, id string) (Node, error) {
	return scanNode(s.DB.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM fs_nodes WHERE id=? AND deleted_at IS NULL`, id))
}
func (s *Store) ListNodes(ctx context.Context, projectID, parentID string) ([]Node, error) {
	query := `SELECT ` + nodeColumns + ` FROM fs_nodes WHERE project_id=? AND parent_id IS NULL AND deleted_at IS NULL ORDER BY node_type DESC,name`
	args := []any{projectID}
	if parentID != "" {
		query = `SELECT ` + nodeColumns + ` FROM fs_nodes WHERE project_id=? AND parent_id=? AND deleted_at IS NULL ORDER BY node_type DESC,name`
		args = append(args, parentID)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, e := scanNode(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) ensureDirectory(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, projectID, parentID string) error {
	if parentID == "" {
		return nil
	}
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM fs_nodes WHERE id=? AND project_id=? AND node_type='directory' AND deleted_at IS NULL`, parentID, projectID).Scan(&one)
	return mapError(err)
}

func (s *Store) CreateDirectory(ctx context.Context, n Node) error {
	name, err := NormalizeName(n.Name)
	if err != nil {
		return err
	}
	if err := s.ensureDirectory(ctx, s.DB, n.ProjectID, n.ParentID.String); err != nil {
		return err
	}
	n.Name = name
	n.NormalizedName = name
	depth, err := s.ParentDepth(ctx, n.ProjectID, n.ParentID.String)
	if err != nil {
		return err
	}
	if depth+1 > 128 {
		return fmt.Errorf("maximum directory depth exceeded")
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO fs_nodes(`+nodeColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, n.ID, n.ProjectID, nullableString(n.ParentID.String), "directory", n.Name, n.NormalizedName, nil, 0, "application/octet-stream", n.CreatedBy, n.CreatedAt, n.UpdatedAt, nil)
	return mapError(err)
}

func (s *Store) ParentDepth(ctx context.Context, projectID, parentID string) (int, error) {
	if parentID == "" {
		return 0, nil
	}
	var depth int
	err := s.DB.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id,parent_id,depth) AS (SELECT id,parent_id,1 FROM fs_nodes WHERE id=? AND project_id=? AND deleted_at IS NULL UNION ALL SELECT n.id,n.parent_id,a.depth+1 FROM fs_nodes n JOIN ancestors a ON n.id=a.parent_id WHERE n.deleted_at IS NULL) SELECT COALESCE(MAX(depth),0) FROM ancestors`, parentID, projectID).Scan(&depth)
	return depth, err
}

func (s *Store) UpdateNode(ctx context.Context, id string, newName, newParentID *string) (Node, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Node{}, err
	}
	defer tx.Rollback()
	n, err := scanNode(tx.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM fs_nodes WHERE id=? AND deleted_at IS NULL`, id))
	if err != nil {
		return Node{}, err
	}
	if newName != nil {
		name, e := NormalizeName(*newName)
		if e != nil {
			return Node{}, e
		}
		n.Name, n.NormalizedName = name, name
	}
	if newParentID != nil {
		if *newParentID == id {
			return Node{}, fmt.Errorf("node cannot be its own parent")
		}
		if err := s.ensureDirectory(ctx, tx, n.ProjectID, *newParentID); err != nil {
			return Node{}, err
		}
		if n.NodeType == "directory" && *newParentID != "" {
			var found int
			e := tx.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (SELECT id FROM fs_nodes WHERE parent_id=? AND deleted_at IS NULL UNION ALL SELECT n.id FROM fs_nodes n JOIN descendants d ON n.parent_id=d.id WHERE n.deleted_at IS NULL) SELECT EXISTS(SELECT 1 FROM descendants WHERE id=?)`, id, *newParentID).Scan(&found)
			if e != nil {
				return Node{}, e
			}
			if found == 1 {
				return Node{}, fmt.Errorf("directory cannot be moved into its descendant")
			}
		}
		parentDepth, e := parentDepthTx(ctx, tx, n.ProjectID, *newParentID)
		if e != nil {
			return Node{}, e
		}
		var subtreeDepth int
		e = tx.QueryRowContext(ctx, `WITH RECURSIVE tree(id,depth) AS (SELECT id,1 FROM fs_nodes WHERE id=? UNION ALL SELECT n.id,t.depth+1 FROM fs_nodes n JOIN tree t ON n.parent_id=t.id WHERE n.deleted_at IS NULL) SELECT MAX(depth) FROM tree`, id).Scan(&subtreeDepth)
		if e != nil {
			return Node{}, e
		}
		if parentDepth+subtreeDepth > 128 {
			return Node{}, fmt.Errorf("maximum directory depth exceeded")
		}
		n.ParentID = sql.NullString{String: *newParentID, Valid: *newParentID != ""}
	}
	n.UpdatedAt = NowMS()
	_, err = tx.ExecContext(ctx, `UPDATE fs_nodes SET name=?,normalized_name=?,parent_id=?,updated_at=? WHERE id=?`, n.Name, n.NormalizedName, nullableString(n.ParentID.String), n.UpdatedAt, id)
	if err != nil {
		return Node{}, mapError(err)
	}
	if err := tx.Commit(); err != nil {
		return Node{}, err
	}
	return n, nil
}

func parentDepthTx(ctx context.Context, tx *sql.Tx, projectID, parentID string) (int, error) {
	if parentID == "" {
		return 0, nil
	}
	var depth int
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id,parent_id,depth) AS (SELECT id,parent_id,1 FROM fs_nodes WHERE id=? AND project_id=? AND deleted_at IS NULL UNION ALL SELECT n.id,n.parent_id,a.depth+1 FROM fs_nodes n JOIN ancestors a ON n.id=a.parent_id WHERE n.deleted_at IS NULL) SELECT COALESCE(MAX(depth),0) FROM ancestors`, parentID, projectID).Scan(&depth)
	return depth, err
}

func (s *Store) DeleteNode(ctx context.Context, id string) error {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var projectID string
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM fs_nodes WHERE id=? AND deleted_at IS NULL`, id).Scan(&projectID); err != nil {
		return mapError(err)
	}
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE tree(id) AS (SELECT ? UNION ALL SELECT n.id FROM fs_nodes n JOIN tree t ON n.parent_id=t.id WHERE n.deleted_at IS NULL) SELECT DISTINCT current_blob_id FROM fs_nodes WHERE id IN tree AND current_blob_id IS NOT NULL`, id)
	if err != nil {
		return err
	}
	var blobs []string
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			rows.Close()
			return err
		}
		blobs = append(blobs, blob)
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `WITH RECURSIVE tree(id) AS (SELECT ? UNION ALL SELECT n.id FROM fs_nodes n JOIN tree t ON n.parent_id=t.id WHERE n.deleted_at IS NULL) UPDATE fs_nodes SET deleted_at=?,updated_at=? WHERE id IN tree`, id, now, now); err != nil {
		return err
	}
	for _, blob := range blobs {
		if _, err := tx.ExecContext(ctx, `UPDATE file_blobs SET status='deleting',deleted_at=? WHERE id=? AND status='active'`, now, blob); err != nil {
			return err
		}
		if err := insertJob(ctx, tx, "delete_blob", map[string]string{"blob_id": blob}, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const blobColumns = `id,project_id,storage_backend_id,object_key,size,mime_type,etag,checksum_algorithm,checksum_value,status,created_at,deleted_at`

func scanBlob(scanner interface{ Scan(...any) error }) (Blob, error) {
	var b Blob
	err := scanner.Scan(&b.ID, &b.ProjectID, &b.StorageBackendID, &b.ObjectKey, &b.Size, &b.MIMEType, &b.ETag, &b.ChecksumAlgorithm, &b.ChecksumValue, &b.Status, &b.CreatedAt, &b.DeletedAt)
	return b, mapError(err)
}
func (s *Store) BlobByID(ctx context.Context, id string) (Blob, error) {
	return scanBlob(s.DB.QueryRowContext(ctx, `SELECT `+blobColumns+` FROM file_blobs WHERE id=?`, id))
}
func (s *Store) BlobForNode(ctx context.Context, nodeID string) (Node, Blob, error) {
	n, err := s.NodeByID(ctx, nodeID)
	if err != nil {
		return Node{}, Blob{}, err
	}
	if n.NodeType != "file" || !n.CurrentBlobID.Valid {
		return Node{}, Blob{}, ErrNotFound
	}
	b, err := s.BlobByID(ctx, n.CurrentBlobID.String)
	if err != nil {
		return Node{}, Blob{}, err
	}
	if b.Status != "active" {
		return Node{}, Blob{}, ErrNotFound
	}
	return n, b, nil
}

func insertJob(ctx context.Context, tx *sql.Tx, jobType string, payload any, now int64) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO background_jobs(id,job_type,payload_json,status,attempts,next_run_at,created_at,updated_at) VALUES(?,?,?,'pending',0,?,?,?)`, security.NewID(), jobType, raw, now, now, now)
	return err
}
