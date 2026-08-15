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

package blobstore_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/kumodo-io/convoy/internal/blobstore"
)

// factory builds a fresh, empty BlobStore for one subtest.
type factory struct {
	name string
	open func(t *testing.T) blobstore.BlobStore
}

// factories runs the conformance suite against every implementation, so the
// in-memory double and the gocloud adapter are held to identical behaviour. The
// gocloud case uses the mem:// driver, exercising the real adapter (error
// mapping, listing, idempotent delete) without a network or a cloud account.
func factories() []factory {
	return []factory{
		{"memory", func(*testing.T) blobstore.BlobStore { return blobstore.NewMemory() }},
		{"gocloud-mem", func(t *testing.T) blobstore.BlobStore {
			t.Helper()
			s, closeFn, err := blobstore.Open(context.Background(), "mem://")
			if err != nil {
				t.Fatalf("Open(mem://): %v", err)
			}
			t.Cleanup(func() { _ = closeFn() })
			return s
		}},
	}
}

func TestConformance(t *testing.T) {
	for _, f := range factories() {
		t.Run(f.name, func(t *testing.T) {
			ctx := context.Background()

			t.Run("PutGetRoundTrip", func(t *testing.T) {
				s := f.open(t)
				want := []byte("hello render")
				if err := s.Put(ctx, "blobs/sha-a", want); err != nil {
					t.Fatalf("Put: %v", err)
				}
				got, err := s.Get(ctx, "blobs/sha-a")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("Get = %q, want %q", got, want)
				}
			})

			t.Run("GetMissingIsErrNotFound", func(t *testing.T) {
				s := f.open(t)
				_, err := s.Get(ctx, "blobs/absent")
				if !errors.Is(err, blobstore.ErrNotFound) {
					t.Errorf("Get(absent) err = %v, want ErrNotFound", err)
				}
			})

			t.Run("Exists", func(t *testing.T) {
				s := f.open(t)
				if err := s.Put(ctx, "k", []byte("v")); err != nil {
					t.Fatalf("Put: %v", err)
				}
				if ok, err := s.Exists(ctx, "k"); err != nil || !ok {
					t.Errorf("Exists(k) = %v,%v; want true,nil", ok, err)
				}
				if ok, err := s.Exists(ctx, "nope"); err != nil || ok {
					t.Errorf("Exists(nope) = %v,%v; want false,nil", ok, err)
				}
			})

			t.Run("ListByPrefixLexical", func(t *testing.T) {
				s := f.open(t)
				// Seed out of order across two prefixes.
				for _, k := range []string{
					"ns/app/history/0002-b.json",
					"ns/app/blobs/sha-b",
					"ns/app/history/0001-a.json",
					"ns/other/blobs/sha-z",
				} {
					if err := s.Put(ctx, k, []byte("x")); err != nil {
						t.Fatalf("Put %q: %v", k, err)
					}
				}
				got, err := s.List(ctx, "ns/app/history/")
				if err != nil {
					t.Fatalf("List: %v", err)
				}
				want := []string{"ns/app/history/0001-a.json", "ns/app/history/0002-b.json"}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("List(history) = %v, want %v (lexical, prefix-scoped)", got, want)
				}
				if all, _ := s.List(ctx, ""); len(all) != 4 {
					t.Errorf("List(\"\") returned %d keys, want 4", len(all))
				}
			})

			t.Run("DeleteRemovesAndIsIdempotent", func(t *testing.T) {
				s := f.open(t)
				if err := s.Put(ctx, "k", []byte("v")); err != nil {
					t.Fatalf("Put: %v", err)
				}
				if err := s.Delete(ctx, "k"); err != nil {
					t.Fatalf("Delete: %v", err)
				}
				if ok, _ := s.Exists(ctx, "k"); ok {
					t.Errorf("key still exists after Delete")
				}
				// Deleting an absent key must not error.
				if err := s.Delete(ctx, "k"); err != nil {
					t.Errorf("second Delete err = %v, want nil (idempotent)", err)
				}
			})

			t.Run("PutOverwrites", func(t *testing.T) {
				s := f.open(t)
				_ = s.Put(ctx, "k", []byte("first"))
				if err := s.Put(ctx, "k", []byte("second")); err != nil {
					t.Fatalf("Put overwrite: %v", err)
				}
				got, _ := s.Get(ctx, "k")
				if string(got) != "second" {
					t.Errorf("Get after overwrite = %q, want %q", got, "second")
				}
			})
		})
	}
}

// TestMemoryGetReturnsCopy guards the in-memory double specifically: a caller
// mutating returned bytes (or the slice it Put) must not corrupt the store.
func TestMemoryGetReturnsCopy(t *testing.T) {
	ctx := context.Background()
	s := blobstore.NewMemory()
	src := []byte("orig")
	if err := s.Put(ctx, "k", src); err != nil {
		t.Fatalf("Put: %v", err)
	}
	src[0] = 'X' // mutate the caller's slice after Put

	got, _ := s.Get(ctx, "k")
	if string(got) != "orig" {
		t.Errorf("store corrupted by caller mutation: got %q", got)
	}
	got[0] = 'Y' // mutate the returned slice
	again, _ := s.Get(ctx, "k")
	if string(again) != "orig" {
		t.Errorf("store corrupted by mutating returned slice: got %q", again)
	}
}
