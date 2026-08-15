/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
)

// bucketStore is the production BlobStore, backed by a gocloud.dev/blob bucket.
// The registered drivers (see drivers.go) decide which URL schemes Open accepts.
type bucketStore struct {
	bucket *blob.Bucket
}

// Open opens the bucket named by a gocloud URL — e.g. "s3://my-bucket",
// "gs://my-bucket", "azblob://my-container", "file:///var/lib/convoy/renders",
// or "mem://" — and returns a BlobStore plus a close func the caller must invoke
// on shutdown. Credentials follow gocloud's per-driver conventions (ambient
// cloud credentials or URL query parameters).
func Open(ctx context.Context, url string) (BlobStore, func() error, error) {
	bucket, err := blob.OpenBucket(ctx, url)
	if err != nil {
		return nil, nil, fmt.Errorf("opening blob bucket %q: %w", url, err)
	}
	return &bucketStore{bucket: bucket}, bucket.Close, nil
}

func (s *bucketStore) Put(ctx context.Context, key string, data []byte) error {
	if err := s.bucket.WriteAll(ctx, key, data, nil); err != nil {
		return fmt.Errorf("writing blob %q: %w", key, err)
	}
	return nil
}

func (s *bucketStore) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := s.bucket.ReadAll(ctx, key)
	if err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("reading blob %q: %w", key, err)
	}
	return data, nil
}

func (s *bucketStore) Exists(ctx context.Context, key string) (bool, error) {
	ok, err := s.bucket.Exists(ctx, key)
	if err != nil {
		return false, fmt.Errorf("checking blob %q: %w", key, err)
	}
	return ok, nil
}

func (s *bucketStore) List(ctx context.Context, prefix string) ([]string, error) {
	// Empty delimiter => flat namespace: every matching key is returned in full,
	// in lexical order (which the render store relies on for ordered history).
	iter := s.bucket.List(&blob.ListOptions{Prefix: prefix})
	var keys []string
	for {
		obj, err := iter.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("listing blobs under %q: %w", prefix, err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

func (s *bucketStore) Delete(ctx context.Context, key string) error {
	err := s.bucket.Delete(ctx, key)
	if err != nil && gcerrors.Code(err) != gcerrors.NotFound {
		return fmt.Errorf("deleting blob %q: %w", key, err)
	}
	return nil
}
