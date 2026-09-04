package storage

import (
	"context"
	"io"
	"time"
)

type Range struct{ Start, End int64 }

type ObjectInfo struct {
	Size              int64
	ETag              string
	ChecksumAlgorithm string
	ChecksumValue     string
}

type CreateUploadRequest struct {
	Key         string
	Size        int64
	ContentType string
	Multipart   bool
	Expires     time.Duration
}

type UploadTarget struct {
	Delivery         string            `json:"delivery"`
	URL              string            `json:"url,omitempty"`
	Method           string            `json:"method,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	ExpiresAt        time.Time         `json:"expires_at,omitempty"`
	ProviderUploadID string            `json:"-"`
}

type PresignPartRequest struct {
	Key              string
	ProviderUploadID string
	PartNumber       int32
	Expires          time.Duration
}

type CompletedPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
}

type DownloadRequest struct {
	Key      string
	Filename string
	Expires  time.Duration
}

type DownloadTarget struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Storage interface {
	Type() string
	Test(context.Context) error
	CreateUpload(context.Context, CreateUploadRequest) (UploadTarget, error)
	PresignUploadPart(context.Context, PresignPartRequest) (UploadTarget, error)
	CompleteMultipartUpload(context.Context, string, string, []CompletedPart) error
	AbortMultipartUpload(context.Context, string, string) error
	Put(context.Context, string, io.Reader, int64, string) error
	Head(context.Context, string) (ObjectInfo, error)
	PresignDownload(context.Context, DownloadRequest) (DownloadTarget, error)
	Open(context.Context, string, *Range) (io.ReadCloser, ObjectInfo, error)
	Delete(context.Context, string) error
}
