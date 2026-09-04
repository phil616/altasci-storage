package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/google/uuid"
)

type Storage struct{ root string }

func New(root string) (*Storage, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("local root_path must be absolute")
	}
	root = filepath.Clean(root)
	for _, dir := range []string{filepath.Join(root, "objects"), filepath.Join(root, "tmp", "uploads")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create local storage directory: %w", err)
		}
	}
	return &Storage{root: root}, nil
}

func (s *Storage) Type() string { return "local" }

func (s *Storage) objectPath(key string) (string, error) {
	if !strings.HasPrefix(key, "v1/objects/") || strings.Contains(key, "..") || strings.ContainsRune(key, '\x00') {
		return "", errors.New("invalid object key")
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(key, "v1/")))
	path := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("object key escapes storage root")
	}
	return path, nil
}

func (s *Storage) Test(ctx context.Context) error {
	key := "v1/objects/.altasci-health/" + uuid.NewString()
	if err := s.Put(ctx, key, strings.NewReader("ok"), 2, "text/plain"); err != nil {
		return err
	}
	if _, err := s.Head(ctx, key); err != nil {
		return err
	}
	r, _, err := s.Open(ctx, key, nil)
	if err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, r)
	closeErr := r.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return s.Delete(ctx, key)
}

func (s *Storage) CreateUpload(context.Context, storageapi.CreateUploadRequest) (storageapi.UploadTarget, error) {
	return storageapi.UploadTarget{Delivery: "local"}, nil
}
func (s *Storage) PresignUploadPart(context.Context, storageapi.PresignPartRequest) (storageapi.UploadTarget, error) {
	return storageapi.UploadTarget{}, errors.New("multipart presign is unsupported for local storage")
}
func (s *Storage) CompleteMultipartUpload(context.Context, string, string, []storageapi.CompletedPart) error {
	return errors.New("multipart is unsupported for local storage")
}
func (s *Storage) AbortMultipartUpload(context.Context, string, string) error { return nil }

func (s *Storage) Put(ctx context.Context, key string, body io.Reader, expected int64, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.objectPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp", "uploads"), "upload-*")
	if err != nil {
		return fmt.Errorf("create upload temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	limited := io.LimitReader(body, expected+1)
	n, copyErr := io.Copy(tmp, limited)
	if copyErr != nil {
		tmp.Close()
		return fmt.Errorf("write upload: %w", copyErr)
	}
	if n != expected {
		tmp.Close()
		return fmt.Errorf("upload size mismatch: got %d, expected %d", n, expected)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync upload: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("commit upload: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err == nil {
		err = dir.Sync()
		dir.Close()
	}
	return err
}

func (s *Storage) Head(ctx context.Context, key string) (storageapi.ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return storageapi.ObjectInfo{}, err
	}
	path, err := s.objectPath(key)
	if err != nil {
		return storageapi.ObjectInfo{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return storageapi.ObjectInfo{}, err
	}
	return storageapi.ObjectInfo{Size: info.Size()}, nil
}

func (s *Storage) PresignDownload(context.Context, storageapi.DownloadRequest) (storageapi.DownloadTarget, error) {
	return storageapi.DownloadTarget{}, errors.New("presigned download is unsupported for local storage")
}

type rangedFile struct {
	file      *os.File
	remaining int64
}

func (r *rangedFile) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.file.Read(p)
	r.remaining -= int64(n)
	return n, err
}
func (r *rangedFile) Close() error { return r.file.Close() }

func (s *Storage) Open(ctx context.Context, key string, rangeSpec *storageapi.Range) (io.ReadCloser, storageapi.ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, storageapi.ObjectInfo{}, err
	}
	path, err := s.objectPath(key)
	if err != nil {
		return nil, storageapi.ObjectInfo{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, storageapi.ObjectInfo{}, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, storageapi.ObjectInfo{}, err
	}
	obj := storageapi.ObjectInfo{Size: info.Size()}
	if rangeSpec == nil {
		return f, obj, nil
	}
	if rangeSpec.Start < 0 || rangeSpec.End < rangeSpec.Start || rangeSpec.End >= info.Size() {
		f.Close()
		return nil, storageapi.ObjectInfo{}, errors.New("invalid range")
	}
	if _, err := f.Seek(rangeSpec.Start, io.SeekStart); err != nil {
		f.Close()
		return nil, storageapi.ObjectInfo{}, err
	}
	return &rangedFile{file: f, remaining: rangeSpec.End - rangeSpec.Start + 1}, obj, nil
}

func (s *Storage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.objectPath(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
