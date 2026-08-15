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

// Package blobstore is convoy's object-store seam: a minimal key→bytes interface
// and its backing implementations. It is the *only* place a concrete storage
// backend is referenced — everything above it (the render store, controllers,
// observer, apiserver) depends on the BlobStore interface and mocks it in tests.
//
// The production implementation is backed by [gocloud.dev/blob], opened from a
// URL (`s3://`, `gs://`, `azblob://`, `file://`, `mem://`), so one implementation
// covers the major clouds natively; anything gocloud does not support can be
// added as another implementation of the same interface. NewMemory is an
// in-process implementation used for dev and as the test double.
package blobstore

import (
	"context"
	"errors"
)

// ErrNotFound is returned by Get when a key has no stored value. Callers use
// errors.Is(err, ErrNotFound) to distinguish "absent" from a transport error.
var ErrNotFound = errors.New("blobstore: key not found")

// BlobStore is a content-addressed object store keyed by slash-delimited paths
// with opaque byte values. It is deliberately minimal: the render store layers
// the blobs/ + history/ layout and all render semantics on top of these five
// operations, so a backend only has to implement dumb object CRUD.
//
// Implementations must be safe for concurrent use.
type BlobStore interface {
	// Put stores data at key, overwriting any existing value. Because keys are
	// content-addressed (the caller derives key from a hash of data), an overwrite
	// re-stores identical bytes and is effectively idempotent.
	Put(ctx context.Context, key string, data []byte) error
	// Get returns the value at key, or ErrNotFound if the key is absent.
	Get(ctx context.Context, key string) ([]byte, error)
	// Exists reports whether key has a stored value.
	Exists(ctx context.Context, key string) (bool, error)
	// List returns all keys under prefix (recursively), in lexical order. A prefix
	// of "" lists the whole store.
	List(ctx context.Context, prefix string) ([]string, error)
	// Delete removes key. It is idempotent: deleting an absent key is not an error.
	Delete(ctx context.Context, key string) error
}
