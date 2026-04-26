package assets

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type LocalStore struct {
	root string
}

func NewLocalStore(root string) *LocalStore {
	if root == "" {
		root = "./data/objects"
	}
	return &LocalStore{root: root}
}

func (s *LocalStore) Put(ctx context.Context, key string, contentType string, body []byte) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return err
	}
	return os.WriteFile(path+".content-type", []byte(contentType), 0o644)
}

func (s *LocalStore) Get(ctx context.Context, key string) ([]byte, string, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	contentTypeBytes, _ := os.ReadFile(path + ".content-type")
	return body, strings.TrimSpace(string(contentTypeBytes)), nil
}

func (s *LocalStore) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if clean == "/" || strings.Contains(clean, "..") {
		return "", errors.New("invalid object key")
	}
	return filepath.Join(s.root, strings.TrimPrefix(clean, "/")), nil
}

type MinIOStore struct {
	client *minio.Client
	bucket string
}

func NewMinIOStore(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinIOStore, error) {
	client, err := minio.New(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://"), &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: useSSL})
	if err != nil {
		return nil, err
	}
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, err
		}
	}
	return &MinIOStore{client: client, bucket: bucket}, nil
}

func (s *MinIOStore) Put(ctx context.Context, key string, contentType string, body []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *MinIOStore) Get(ctx context.Context, key string) ([]byte, string, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", err
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		return nil, "", err
	}
	body, err := io.ReadAll(object)
	return body, info.ContentType, err
}
