package repository

import (
	"context"
	"database/sql"
)

type Job struct {
	ID, JobType, PayloadJSON, Status string
	Attempts                         int
	NextRunAt, CreatedAt, UpdatedAt  int64
	LastError                        sql.NullString
}

func (s *Store) ClaimJobs(ctx context.Context, now int64, limit int) ([]Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,job_type,payload_json,status,attempts,next_run_at,created_at,updated_at,last_error FROM background_jobs WHERE status='pending' AND next_run_at<=? ORDER BY next_run_at LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.JobType, &j.PayloadJSON, &j.Status, &j.Attempts, &j.NextRunAt, &j.CreatedAt, &j.UpdatedAt, &j.LastError); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, j)
	}
	rows.Close()
	for i := range out {
		res, err := tx.ExecContext(ctx, `UPDATE background_jobs SET status='running',updated_at=? WHERE id=? AND status='pending'`, now, out[i].ID)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue
		}
		out[i].Status = "running"
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Store) RecoverRunningJobs(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE background_jobs SET status='pending',updated_at=? WHERE status='running'`, NowMS())
	return err
}
func (s *Store) CompleteJob(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE background_jobs SET status='done',updated_at=?,last_error=NULL WHERE id=?`, NowMS(), id)
	return err
}
func (s *Store) FailJob(ctx context.Context, id, message string, attempts int, nextRun int64) error {
	status := "pending"
	if attempts >= 10 {
		status = "failed"
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE background_jobs SET status=?,attempts=?,next_run_at=?,updated_at=?,last_error=? WHERE id=?`, status, attempts, nextRun, NowMS(), message, id)
	return err
}
func (s *Store) MarkBlobDeleted(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE file_blobs SET status='deleted',deleted_at=? WHERE id=?`, NowMS(), id)
	return err
}
func (s *Store) ExpireUpload(ctx context.Context, id string) error {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var blobID string
	err = tx.QueryRowContext(ctx, `SELECT blob_id FROM upload_sessions WHERE id=? AND status IN ('created','uploading','completing')`, id).Scan(&blobID)
	if err != nil {
		return mapError(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_sessions SET status='expired' WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file_blobs SET status='deleting',deleted_at=? WHERE id=?`, now, blobID); err != nil {
		return err
	}
	return tx.Commit()
}
