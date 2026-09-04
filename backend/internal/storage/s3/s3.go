package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	storageapi "github.com/altasci/network-storage/backend/internal/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
)

type Config struct {
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	Bucket         string `json:"bucket"`
	Prefix         string `json:"prefix"`
	ForcePathStyle bool   `json:"force_path_style"`
}
type Secret struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

type Storage struct {
	client         *awss3.Client
	presign        *awss3.PresignClient
	bucket, prefix string
}

func New(ctx context.Context, cfg Config, secret Secret) (*Storage, error) {
	if cfg.Region == "" || cfg.Bucket == "" || secret.AccessKeyID == "" || secret.SecretAccessKey == "" {
		return nil, fmt.Errorf("region, bucket and credentials are required")
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(secret.AccessKeyID, secret.SecretAccessKey, "")))
	if err != nil {
		return nil, fmt.Errorf("load S3 config: %w", err)
	}
	client := awss3.NewFromConfig(loaded, func(o *awss3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})
	return &Storage{client: client, presign: awss3.NewPresignClient(client), bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/")}, nil
}

func (s *Storage) Type() string { return "s3" }
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
		out, err := s.client.CreateMultipartUpload(ctx, &awss3.CreateMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(req.ContentType)})
		if err != nil {
			return storageapi.UploadTarget{}, fmt.Errorf("initiate S3 multipart upload: %w", err)
		}
		return storageapi.UploadTarget{Delivery: "external", ProviderUploadID: aws.ToString(out.UploadId)}, nil
	}
	out, err := s.presign.PresignPutObject(ctx, &awss3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(req.ContentType), ContentLength: aws.Int64(req.Size)}, func(o *awss3.PresignOptions) { o.Expires = req.Expires })
	if err != nil {
		return storageapi.UploadTarget{}, fmt.Errorf("presign S3 upload: %w", err)
	}
	return storageapi.UploadTarget{Delivery: "external", URL: out.URL, Method: out.Method, Headers: flatten(out.SignedHeader), ExpiresAt: time.Now().UTC().Add(req.Expires)}, nil
}

func (s *Storage) PresignUploadPart(ctx context.Context, req storageapi.PresignPartRequest) (storageapi.UploadTarget, error) {
	out, err := s.presign.PresignUploadPart(ctx, &awss3.UploadPartInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(req.Key)), UploadId: aws.String(req.ProviderUploadID), PartNumber: aws.Int32(req.PartNumber)}, func(o *awss3.PresignOptions) { o.Expires = req.Expires })
	if err != nil {
		return storageapi.UploadTarget{}, fmt.Errorf("presign S3 upload part: %w", err)
	}
	return storageapi.UploadTarget{Delivery: "external", URL: out.URL, Method: out.Method, Headers: flatten(out.SignedHeader), ExpiresAt: time.Now().UTC().Add(req.Expires)}, nil
}

func (s *Storage) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []storageapi.CompletedPart) error {
	completed := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		completed[i] = types.CompletedPart{PartNumber: aws.Int32(p.PartNumber), ETag: aws.String(p.ETag)}
	}
	_, err := s.client.CompleteMultipartUpload(ctx, &awss3.CompleteMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)), UploadId: aws.String(uploadID), MultipartUpload: &types.CompletedMultipartUpload{Parts: completed}})
	if err != nil {
		return fmt.Errorf("complete S3 multipart upload: %w", err)
	}
	return nil
}

func (s *Storage) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	_, err := s.client.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)), UploadId: aws.String(uploadID)})
	return err
}

func (s *Storage) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, &awss3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)), Body: body, ContentLength: aws.Int64(size), ContentType: aws.String(contentType)})
	if err != nil {
		return fmt.Errorf("put S3 object: %w", err)
	}
	return nil
}

func (s *Storage) Head(ctx context.Context, key string) (storageapi.ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))})
	if err != nil {
		return storageapi.ObjectInfo{}, fmt.Errorf("head S3 object: %w", err)
	}
	return storageapi.ObjectInfo{Size: aws.ToInt64(out.ContentLength), ETag: aws.ToString(out.ETag)}, nil
}

func (s *Storage) PresignDownload(ctx context.Context, req storageapi.DownloadRequest) (storageapi.DownloadTarget, error) {
	disposition := "attachment; filename*=UTF-8''" + url.PathEscape(req.Filename)
	out, err := s.presign.PresignGetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(req.Key)), ResponseContentDisposition: aws.String(disposition)}, func(o *awss3.PresignOptions) { o.Expires = req.Expires })
	if err != nil {
		return storageapi.DownloadTarget{}, fmt.Errorf("presign S3 download: %w", err)
	}
	return storageapi.DownloadTarget{URL: out.URL, ExpiresAt: time.Now().UTC().Add(req.Expires)}, nil
}

func (s *Storage) Open(ctx context.Context, key string, rng *storageapi.Range) (io.ReadCloser, storageapi.ObjectInfo, error) {
	in := &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))}
	if rng != nil {
		in.Range = aws.String(fmt.Sprintf("bytes=%d-%d", rng.Start, rng.End))
	}
	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		return nil, storageapi.ObjectInfo{}, fmt.Errorf("get S3 object: %w", err)
	}
	return out.Body, storageapi.ObjectInfo{Size: aws.ToInt64(out.ContentLength), ETag: aws.ToString(out.ETag)}, nil
}

func (s *Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))})
	if err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func flatten(h map[string][]string) map[string]string {
	result := map[string]string{}
	for k, v := range h {
		result[k] = strings.Join(v, ",")
	}
	return result
}
