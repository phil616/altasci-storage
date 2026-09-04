package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/storage/factory"
)

type Worker struct {
	store   *repository.Store
	factory *factory.Factory
	log     *slog.Logger
}

func New(store *repository.Store, factory *factory.Factory, log *slog.Logger) *Worker {
	return &Worker{store: store, factory: factory, log: log}
}
func (w *Worker) Run(ctx context.Context) {
	jobs := time.NewTicker(10 * time.Second)
	cleanup := time.NewTicker(5 * time.Minute)
	defer jobs.Stop()
	defer cleanup.Stop()
	if err := w.store.RecoverRunningJobs(ctx); err != nil { w.log.Error("recover interrupted background jobs", "error", err) }
	w.runJobs(ctx)
	w.cleanupUploads(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-jobs.C:
			w.runJobs(ctx)
		case <-cleanup.C:
			w.cleanupUploads(ctx)
		}
	}
}
func (w *Worker) runJobs(ctx context.Context) {
	items, err := w.store.ClaimJobs(ctx, repository.NowMS(), 20)
	if err != nil {
		w.log.Error("claim background jobs", "error", err)
		return
	}
	for _, job := range items {
		err = w.execute(ctx, job)
		if err == nil {
			err = w.store.CompleteJob(ctx, job.ID)
		} else {
			attempt := job.Attempts + 1
			delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}
			delay := delays[min(attempt-1, len(delays)-1)]
			_ = w.store.FailJob(ctx, job.ID, err.Error(), attempt, time.Now().Add(delay).UnixMilli())
		}
		if err != nil {
			w.log.Error("background job failed", "job_id", job.ID, "job_type", job.JobType, "error", err)
		}
	}
}
func (w *Worker) execute(ctx context.Context, job repository.Job) error {
	switch job.JobType {
	case "delete_blob":
		var payload struct {
			BlobID string `json:"blob_id"`
		}
		if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
			return err
		}
		blob, err := w.store.BlobByID(ctx, payload.BlobID)
		if err != nil {
			return err
		}
		adapter, backend, err := w.factory.ByID(ctx, blob.StorageBackendID)
		if err != nil {
			return err
		}
		if !backend.Enabled {
			return repository.ErrConflict
		}
		if err := adapter.Delete(ctx, blob.ObjectKey); err != nil {
			return err
		}
		return w.store.MarkBlobDeleted(ctx, blob.ID)
	default:
		return nil
	}
}
func (w *Worker) cleanupUploads(ctx context.Context) {
	if err := w.store.PurgeExpiredOIDCFlows(ctx, repository.NowMS()); err != nil {
		w.log.Error("purge expired OIDC flows", "error", err)
	}
	uploads, err := w.store.ExpiredUploads(ctx, repository.NowMS(), 100)
	if err != nil {
		w.log.Error("list expired uploads", "error", err)
		return
	}
	for _, u := range uploads {
		blob, err := w.store.BlobByID(ctx, u.BlobID)
		if err != nil {
			continue
		}
		adapter, _, err := w.factory.ByID(ctx, blob.StorageBackendID)
		if err != nil {
			continue
		}
		if u.UploadType == "multipart" && u.ProviderUploadID.Valid {
			err = adapter.AbortMultipartUpload(ctx, blob.ObjectKey, u.ProviderUploadID.String)
		} else {
			err = adapter.Delete(ctx, blob.ObjectKey)
		}
		if err != nil {
			w.log.Error("cleanup expired upload", "upload_id", u.ID, "error", err)
			continue
		}
		if err := w.store.ExpireUpload(ctx, u.ID); err != nil {
			w.log.Error("expire upload metadata", "upload_id", u.ID, "error", err)
		}
	}
}
