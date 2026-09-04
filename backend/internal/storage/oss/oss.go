package oss

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/google/uuid"
)

type Config struct {
	Region   string `json:"region"`
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix"`
	UseCName bool   `json:"use_cname"`
}
type Secret struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
}
type Storage struct {
	client         *aliyun.Client
	bucket, prefix string
}

func New(cfg Config, secret Secret) (*Storage, error) {
	if cfg.Region == "" || cfg.Bucket == "" || secret.AccessKeyID == "" || secret.AccessKeySecret == "" {
		return nil, fmt.Errorf("region, bucket and credentials are required")
	}
	c := aliyun.LoadDefaultConfig().WithRegion(cfg.Region).WithCredentialsProvider(credentials.NewStaticCredentialsProvider(secret.AccessKeyID, secret.AccessKeySecret))
	if cfg.Endpoint != "" {
		c.WithEndpoint(cfg.Endpoint)
	}
	c.WithUseCName(cfg.UseCName)
	return &Storage{client: aliyun.NewClient(c), bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/")}, nil
}
func (s *Storage) Type() string { return "aliyun_oss" }
func (s *Storage) key(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}
func (s *Storage) Test(ctx context.Context) error {
	key := ".altasci-health/" + uuid.NewString()
	if err := s.Put(ctx, key, bytes.NewReader([]byte("ok")), 2, "text/plain"); err != nil {
		return err
	}
	defer s.Delete(context.WithoutCancel(ctx), key)
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
	return closeErr
}
func (s *Storage) CreateUpload(ctx context.Context, req storageapi.CreateUploadRequest) (storageapi.UploadTarget, error) {
	key := s.key(req.Key)
	if req.Multipart {
		out, err := s.client.InitiateMultipartUpload(ctx, &aliyun.InitiateMultipartUploadRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(key), ContentType: aliyun.Ptr(req.ContentType)})
		if err != nil {
			return storageapi.UploadTarget{}, fmt.Errorf("initiate OSS multipart upload: %w", err)
		}
		return storageapi.UploadTarget{Delivery: "external", ProviderUploadID: aliyun.ToString(out.UploadId)}, nil
	}
	out, err := s.client.Presign(ctx, &aliyun.PutObjectRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(key), ContentType: aliyun.Ptr(req.ContentType), ContentLength: aliyun.Ptr(req.Size)}, aliyun.PresignExpires(req.Expires))
	if err != nil {
		return storageapi.UploadTarget{}, fmt.Errorf("presign OSS upload: %w", err)
	}
	return storageapi.UploadTarget{Delivery: "external", URL: out.URL, Method: out.Method, Headers: out.SignedHeaders, ExpiresAt: out.Expiration}, nil
}
func (s *Storage) PresignUploadPart(ctx context.Context, req storageapi.PresignPartRequest) (storageapi.UploadTarget, error) {
	out, err := s.client.Presign(ctx, &aliyun.UploadPartRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(s.key(req.Key)), UploadId: aliyun.Ptr(req.ProviderUploadID), PartNumber: req.PartNumber}, aliyun.PresignExpires(req.Expires))
	if err != nil {
		return storageapi.UploadTarget{}, fmt.Errorf("presign OSS upload part: %w", err)
	}
	return storageapi.UploadTarget{Delivery: "external", URL: out.URL, Method: out.Method, Headers: out.SignedHeaders, ExpiresAt: out.Expiration}, nil
}
func (s *Storage) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []storageapi.CompletedPart) error {
	uploaded := make([]aliyun.UploadPart, len(parts))
	for i, p := range parts {
		uploaded[i] = aliyun.UploadPart{PartNumber: p.PartNumber, ETag: aliyun.Ptr(p.ETag)}
	}
	_, err := s.client.CompleteMultipartUpload(ctx, &aliyun.CompleteMultipartUploadRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(s.key(key)), UploadId: aliyun.Ptr(uploadID), CompleteMultipartUpload: &aliyun.CompleteMultipartUpload{Parts: uploaded}})
	if err != nil {
		return fmt.Errorf("complete OSS multipart upload: %w", err)
	}
	return nil
}
func (s *Storage) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	_, err := s.client.AbortMultipartUpload(ctx, &aliyun.AbortMultipartUploadRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(s.key(key)), UploadId: aliyun.Ptr(uploadID)})
	return err
}
func (s *Storage) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, &aliyun.PutObjectRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(s.key(key)), Body: body, ContentLength: aliyun.Ptr(size), ContentType: aliyun.Ptr(contentType)})
	if err != nil {
		return fmt.Errorf("put OSS object: %w", err)
	}
	return nil
}
func (s *Storage) Head(ctx context.Context, key string) (storageapi.ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &aliyun.HeadObjectRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(s.key(key))})
	if err != nil {
		return storageapi.ObjectInfo{}, fmt.Errorf("head OSS object: %w", err)
	}
	return storageapi.ObjectInfo{Size: out.ContentLength, ETag: aliyun.ToString(out.ETag), ChecksumAlgorithm: "crc64ecma", ChecksumValue: aliyun.ToString(out.HashCRC64)}, nil
}
func (s *Storage) PresignDownload(ctx context.Context, req storageapi.DownloadRequest) (storageapi.DownloadTarget, error) {
	disposition := "attachment; filename*=UTF-8''" + url.PathEscape(req.Filename)
	out, err := s.client.Presign(ctx, &aliyun.GetObjectRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(s.key(req.Key)), ResponseContentDisposition: aliyun.Ptr(disposition)}, aliyun.PresignExpires(req.Expires))
	if err != nil {
		return storageapi.DownloadTarget{}, fmt.Errorf("presign OSS download: %w", err)
	}
	return storageapi.DownloadTarget{URL: out.URL, ExpiresAt: out.Expiration}, nil
}
func (s *Storage) Open(ctx context.Context, key string, rng *storageapi.Range) (io.ReadCloser, storageapi.ObjectInfo, error) {
	req := &aliyun.GetObjectRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(s.key(key))}
	if rng != nil {
		req.Range = aliyun.Ptr(fmt.Sprintf("bytes=%d-%d", rng.Start, rng.End))
	}
	out, err := s.client.GetObject(ctx, req)
	if err != nil {
		return nil, storageapi.ObjectInfo{}, fmt.Errorf("get OSS object: %w", err)
	}
	return out.Body, storageapi.ObjectInfo{Size: out.ContentLength, ETag: aliyun.ToString(out.ETag)}, nil
}
func (s *Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &aliyun.DeleteObjectRequest{Bucket: aliyun.Ptr(s.bucket), Key: aliyun.Ptr(s.key(key))})
	if err != nil {
		return fmt.Errorf("delete OSS object: %w", err)
	}
	return nil
}
