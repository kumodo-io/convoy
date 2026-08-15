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

// Package scopedinformers provides shard-scoped informers for the high-cardinality
// per-Release kinds (Release, ReleaseInventory, Cluster). Each replica caches only
// the objects for the shards it currently owns, so per-replica memory scales with
// the fleet's replica count rather than the total object count. See
// docs/architecture/05-shard-scoped-caches.md.
//
// The mechanism: one filtered SharedInformerFactory per kind whose LIST/WATCH
// carries a `convoy.kumodo.io/shard in (owned…)` label selector. When shard
// ownership changes, Rebuild builds a fresh factory with the new selector, syncs
// it, atomically swaps the backing informers, and stops the old factory
// (build-new → sync → swap → stop-old; never a serving gap).
//
// Consumers (controllers, the inventory store) hold the typed informer/lister
// facades this package hands out; those facades survive rebuilds by delegating to
// the current backing informer, so callers wire once and never see the churn.
package scopedinformers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	coreinformers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	corelisters "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/sharding"
)

// swappableInformer is a cache.SharedIndexInformer facade whose backing informer
// can be replaced at runtime (on shard-ownership change). Event handlers and
// indexers registered through it are recorded and replayed onto every new backing
// informer, so a consumer registers once (at construction, before the first
// backing exists) and keeps receiving events across rebuilds.
//
// The embedded SharedIndexInformer supplies the bulk of the interface by
// promotion; only the methods needing record/replay (AddEventHandler,
// AddIndexers) or lock-guarded, nil-safe access to the current backing
// (GetIndexer, HasSynced) are overridden.
type swappableInformer struct {
	mu sync.RWMutex
	// SharedIndexInformer is the current backing informer (nil until the first
	// Rebuild). Swapped under mu in setBacking.
	cache.SharedIndexInformer
	handlers []cache.ResourceEventHandler
	indexers []cache.Indexers
	// emptyIndexer answers GetIndexer before the first backing exists, so a lister
	// read during the cold-start window returns empty rather than panicking.
	emptyIndexer cache.Indexer
}

func newSwappableInformer() *swappableInformer {
	return &swappableInformer{
		emptyIndexer: cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
	}
}

// AddEventHandler records the handler and (if a backing already exists) registers
// it. The returned registration is nil: all callers here discard it. On the next
// Rebuild the handler is replayed onto the new backing.
func (s *swappableInformer) AddEventHandler(h cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, h)
	if s.SharedIndexInformer != nil {
		return s.SharedIndexInformer.AddEventHandler(h)
	}
	return nil, nil
}

// AddIndexers records the indexers and (if a backing already exists) applies them.
// Replayed onto each new backing before it starts.
func (s *swappableInformer) AddIndexers(indexers cache.Indexers) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexers = append(s.indexers, indexers)
	if s.SharedIndexInformer != nil {
		return s.SharedIndexInformer.AddIndexers(indexers)
	}
	return nil
}

// GetIndexer returns the current backing's indexer, or an empty one before the
// first Rebuild. Listers read through here, so it must be lock-guarded against a
// concurrent swap.
func (s *swappableInformer) GetIndexer() cache.Indexer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.SharedIndexInformer == nil {
		return s.emptyIndexer
	}
	return s.SharedIndexInformer.GetIndexer()
}

// HasSynced reports the current backing's sync state (false before the first
// Rebuild).
func (s *swappableInformer) HasSynced() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SharedIndexInformer != nil && s.SharedIndexInformer.HasSynced()
}

// prepare replays the recorded indexers and handlers onto a freshly-built backing
// informer. Called before the informer starts, so its initial LIST populates the
// index and delivers to every handler.
func (s *swappableInformer) prepare(inf cache.SharedIndexInformer) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, idx := range s.indexers {
		if err := inf.AddIndexers(idx); err != nil {
			return err
		}
	}
	for _, h := range s.handlers {
		if _, err := inf.AddEventHandler(h); err != nil {
			return err
		}
	}
	return nil
}

// setBacking atomically swaps in the (already-synced) new backing informer.
func (s *swappableInformer) setBacking(inf cache.SharedIndexInformer) {
	s.mu.Lock()
	s.SharedIndexInformer = inf
	s.mu.Unlock()
}

// Typed facades. Each implements the generated typed-informer interface, so the
// controllers and stores that take a coreinformers.XInformer accept them
// unchanged. Listers are rebuilt per call over the current backing indexer (a
// lister is a cheap stateless wrapper), so reads always hit the live cache.

type releaseInformer struct{ inf *swappableInformer }

func (r releaseInformer) Informer() cache.SharedIndexInformer { return r.inf }
func (r releaseInformer) Lister() corelisters.ReleaseLister   { return releaseLister(r) }

type releaseLister struct{ inf *swappableInformer }

func (l releaseLister) List(sel labels.Selector) ([]*corev1alpha1.Release, error) {
	return corelisters.NewReleaseLister(l.inf.GetIndexer()).List(sel)
}

func (l releaseLister) Releases(namespace string) corelisters.ReleaseNamespaceLister {
	return corelisters.NewReleaseLister(l.inf.GetIndexer()).Releases(namespace)
}

type clusterInformer struct{ inf *swappableInformer }

func (c clusterInformer) Informer() cache.SharedIndexInformer { return c.inf }
func (c clusterInformer) Lister() corelisters.ClusterLister   { return clusterLister(c) }

type clusterLister struct{ inf *swappableInformer }

func (l clusterLister) List(sel labels.Selector) ([]*corev1alpha1.Cluster, error) {
	return corelisters.NewClusterLister(l.inf.GetIndexer()).List(sel)
}

func (l clusterLister) Clusters(namespace string) corelisters.ClusterNamespaceLister {
	return corelisters.NewClusterLister(l.inf.GetIndexer()).Clusters(namespace)
}

type releaseInventoryInformer struct{ inf *swappableInformer }

func (r releaseInventoryInformer) Informer() cache.SharedIndexInformer { return r.inf }
func (r releaseInventoryInformer) Lister() corelisters.ReleaseInventoryLister {
	return releaseInventoryLister(r)
}

type releaseInventoryLister struct{ inf *swappableInformer }

func (l releaseInventoryLister) List(sel labels.Selector) ([]*corev1alpha1.ReleaseInventory, error) {
	return corelisters.NewReleaseInventoryLister(l.inf.GetIndexer()).List(sel)
}

func (l releaseInventoryLister) ReleaseInventories(namespace string) corelisters.ReleaseInventoryNamespaceLister {
	return corelisters.NewReleaseInventoryLister(l.inf.GetIndexer()).ReleaseInventories(namespace)
}

// Set manages the shard-scoped informers for the three per-Release kinds. It hands
// out stable typed facades (Releases/Clusters/ReleaseInventories) and rebuilds the
// backing filtered factory whenever shard ownership changes.
type Set struct {
	client clientset.Interface
	resync time.Duration
	// scoped selects the mode: true installs a `shard in (owned…)` label selector
	// (per-shard caching); false uses no selector (full cache — the safe default,
	// behaviourally identical to the pre-sharding informers).
	scoped bool

	release   *swappableInformer
	cluster   *swappableInformer
	inventory *swappableInformer

	mu           sync.Mutex
	lastSelector string
	built        bool
	cancelOld    context.CancelFunc
}

// New returns a Set. scoped=false keeps a full cache (default); scoped=true caches
// only owned shards.
func New(client clientset.Interface, resync time.Duration, scoped bool) *Set {
	return &Set{
		client:    client,
		resync:    resync,
		scoped:    scoped,
		release:   newSwappableInformer(),
		cluster:   newSwappableInformer(),
		inventory: newSwappableInformer(),
	}
}

// Releases returns the shard-scoped Release informer facade.
func (s *Set) Releases() coreinformers.ReleaseInformer { return releaseInformer{s.release} }

// Clusters returns the shard-scoped Cluster informer facade.
func (s *Set) Clusters() coreinformers.ClusterInformer { return clusterInformer{s.cluster} }

// ReleaseInventories returns the shard-scoped ReleaseInventory informer facade.
func (s *Set) ReleaseInventories() coreinformers.ReleaseInventoryInformer {
	return releaseInventoryInformer{s.inventory}
}

// selector returns the label-selector string for the given owned shards. Full-cache
// mode returns "" (match everything). In scoped mode an empty owned set yields a
// never-match selector (an empty cache) rather than an invalid `in ()`.
func (s *Set) selector(owned []int) string {
	if !s.scoped {
		return ""
	}
	if len(owned) == 0 {
		// Shard values are non-negative integers, so "none" is never a real shard:
		// this selector matches nothing (an empty cache) while staying a valid,
		// parseable label value.
		return sharding.ShardLabel + " in (none)"
	}
	vals := make([]string, len(owned))
	for i, sh := range owned {
		vals[i] = strconv.Itoa(sh)
	}
	return sharding.ShardLabel + " in (" + strings.Join(vals, ",") + ")"
}

// Rebuild builds a new filtered factory for the owned shards, waits for its caches
// to sync, atomically swaps the backing informers, and stops the previous factory.
// It is a no-op if the resulting selector is unchanged since the last successful
// build, so calling it once explicitly after Prime is safe even when an ownership
// OnChange already triggered it.
//
// Rebuild must complete once before the informers serve reads; call it after the
// coordinator has primed ownership and before starting the controllers.
func (s *Set) Rebuild(ctx context.Context, owned []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sel := s.selector(owned)
	if s.built && sel == s.lastSelector {
		return nil
	}

	tweak := func(o *metav1.ListOptions) {
		if sel != "" {
			o.LabelSelector = sel
		}
	}
	factory := informers.NewSharedInformerFactoryWithOptions(s.client, s.resync,
		informers.WithTweakListOptions(tweak))

	relInf := factory.Core().V1alpha1().Releases().Informer()
	cluInf := factory.Core().V1alpha1().Clusters().Informer()
	invInf := factory.Core().V1alpha1().ReleaseInventories().Informer()

	// Replay recorded indexers + handlers onto the new backings before starting,
	// so the initial LIST both populates the index and drives the handlers.
	if err := s.release.prepare(relInf); err != nil {
		return fmt.Errorf("preparing release informer: %w", err)
	}
	if err := s.cluster.prepare(cluInf); err != nil {
		return fmt.Errorf("preparing cluster informer: %w", err)
	}
	if err := s.inventory.prepare(invInf); err != nil {
		return fmt.Errorf("preparing release inventory informer: %w", err)
	}

	// The new factory runs on its own cancellable context so we can stop it
	// independently. We only cancel the *previous* factory after the new one has
	// synced and been swapped in — never a serving gap, only brief double-caching.
	fctx, cancel := context.WithCancel(ctx)
	factory.Start(fctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), relInf.HasSynced, cluInf.HasSynced, invInf.HasSynced) {
		cancel()
		return fmt.Errorf("scopedinformers: timed out waiting for caches to sync")
	}

	s.release.setBacking(relInf)
	s.cluster.setBacking(cluInf)
	s.inventory.setBacking(invInf)

	if s.cancelOld != nil {
		s.cancelOld()
	}
	s.cancelOld = cancel
	s.lastSelector = sel
	s.built = true
	return nil
}
