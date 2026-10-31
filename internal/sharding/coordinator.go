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

package sharding

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	// labelComponent scopes leases to a sharded component (controller-manager vs
	// observer), so each maintains an independent membership + shard set.
	labelComponent = "sharding.convoy.kumodo.io/component"
	// labelRole distinguishes a per-replica member lease from a shard lease.
	labelRole = "sharding.convoy.kumodo.io/role"

	roleMember = "member"
	roleShard  = "shard"

	// freeHolder is the holder-identity value that marks a shard lease as
	// available. A real holder is always a non-empty replica ID.
	freeHolder = ""

	// memberGCMultiplier sets how far past its validity a dead replica's member
	// lease must be before it is reclaimed: LeaseDuration × this. Comfortably
	// larger than the liveness window so a briefly-partitioned live replica is
	// never GC'd (and if it were, it simply recreates its lease on the next tick).
	memberGCMultiplier = 10
)

// Ownership answers, on the reconcile hot path, whether the local replica owns a
// given shard. Controllers gate their work on Owns; OnChange lets a manager
// re-drive newly-owned work when the assignment shifts.
type Ownership interface {
	// Owns reports whether the local replica currently owns shard.
	Owns(shard int) bool
	// OwnedShards returns the currently-owned shard indices (ascending).
	OwnedShards() []int
	// OnChange registers a callback fired whenever the owned set changes.
	// Callbacks must be registered before Run.
	OnChange(fn func())
}

// Config parameterizes a Coordinator.
type Config struct {
	// Component names the sharded component (e.g. "convoy-cm", "convoy-obs").
	// Leases are labelled and named with it, keeping components independent.
	Component string
	// ReplicaID is this replica's unique holder identity (typically the pod name).
	ReplicaID string
	// Namespace is where member and shard Leases live.
	Namespace string
	// Shards is the fixed shard count S. Must match across the fleet.
	Shards int
	// LeaseDuration is how long a held lease is considered valid without renewal.
	LeaseDuration time.Duration
	// RenewInterval is how often the loop renews leases and re-evaluates ownership.
	RenewInterval time.Duration
}

// Coordinator maintains the local replica's shard ownership via Kubernetes
// Leases: a heartbeated member lease establishes live membership, and shard
// leases enforce exactly-one ownership per shard. It implements Ownership.
type Coordinator struct {
	client  kubernetes.Interface
	cfg     Config
	sharder Sharder
	now     func() time.Time

	mu       sync.RWMutex
	owned    map[int]bool
	onChange []func()
}

// NewCoordinator builds a Coordinator. It does not start the loop; call Run.
func NewCoordinator(client kubernetes.Interface, cfg Config) *Coordinator {
	if cfg.Shards < 1 {
		cfg.Shards = 1
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 30 * time.Second
	}
	if cfg.RenewInterval <= 0 {
		cfg.RenewInterval = 10 * time.Second
	}
	return &Coordinator{
		client:  client,
		cfg:     cfg,
		sharder: NewSharder(cfg.Shards),
		now:     time.Now,
		owned:   make(map[int]bool),
	}
}

// Sharder returns the Sharder over this Coordinator's shard count, so callers
// map a work identity to a shard consistently with ownership.
func (c *Coordinator) Sharder() Sharder {
	return c.sharder
}

// Owns implements Ownership.
func (c *Coordinator) Owns(shard int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.owned[shard]
}

// OwnedShards implements Ownership.
func (c *Coordinator) OwnedShards() []int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]int, 0, len(c.owned))
	for s := range c.owned {
		out = append(out, s)
	}
	sort.Ints(out)
	return out
}

// OnChange implements Ownership.
func (c *Coordinator) OnChange(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onChange = append(c.onChange, fn)
}

// Prime performs one synchronous membership + ownership evaluation. Call it
// before starting informers so the enqueue filters see correct ownership from
// the first event, avoiding a cold-start gap.
func (c *Coordinator) Prime(ctx context.Context) {
	c.tick(ctx)
}

// Run drives the membership + shard-lease loop until ctx is cancelled, then
// releases all held leases so failover is immediate rather than TTL-delayed.
func (c *Coordinator) Run(ctx context.Context) error {
	klog.InfoS("starting sharding coordinator", "component", c.cfg.Component,
		"replicaID", c.cfg.ReplicaID, "shards", c.cfg.Shards)

	c.tick(ctx)

	ticker := time.NewTicker(c.cfg.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.releaseAll()
			klog.InfoS("stopped sharding coordinator", "component", c.cfg.Component)
			return nil
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

// tick renews the member lease, recomputes desired shards from live membership,
// and acquires/releases shard leases to match.
func (c *Coordinator) tick(ctx context.Context) {
	if err := c.renewMemberLease(ctx); err != nil {
		klog.ErrorS(err, "renewing member lease", "component", c.cfg.Component)
	}

	members, err := c.liveMembers(ctx)
	if err != nil {
		klog.ErrorS(err, "listing members", "component", c.cfg.Component)
		return
	}
	myIndex := indexOf(members, c.cfg.ReplicaID)
	desired := desiredShards(myIndex, len(members), c.cfg.Shards)

	newOwned := make(map[int]bool, len(desired))
	for i := 0; i < c.cfg.Shards; i++ {
		switch {
		case desired[i]:
			if c.acquireOrRenew(ctx, i) {
				newOwned[i] = true
			}
		case c.Owns(i):
			c.release(ctx, i)
		}
	}
	c.setOwned(newOwned)
}

// setOwned swaps the owned set and fires OnChange callbacks if it changed.
func (c *Coordinator) setOwned(newOwned map[int]bool) {
	c.mu.Lock()
	changed := !equalIntSet(c.owned, newOwned)
	c.owned = newOwned
	callbacks := c.onChange
	c.mu.Unlock()
	if changed {
		klog.InfoS("shard ownership changed", "component", c.cfg.Component,
			"replicaID", c.cfg.ReplicaID, "owned", sortedKeys(newOwned))
		for _, fn := range callbacks {
			fn()
		}
	}
}

// renewMemberLease upserts this replica's member lease with a fresh renew time.
func (c *Coordinator) renewMemberLease(ctx context.Context) error {
	name := c.memberLeaseName()
	leases := c.client.CoordinationV1().Leases(c.cfg.Namespace)
	now := metav1.NewMicroTime(c.now())

	lease, err := leases.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, cerr := leases.Create(ctx, c.newLease(name, roleMember, c.cfg.ReplicaID, now), metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(cerr) {
			return nil // created concurrently; next tick renews it
		}
		return cerr
	}
	if err != nil {
		return err
	}
	lease.Spec.HolderIdentity = new(c.cfg.ReplicaID)
	lease.Spec.RenewTime = &now
	_, err = leases.Update(ctx, lease, metav1.UpdateOptions{})
	return err
}

// liveMembers returns the sorted, deduplicated holder identities of member
// leases whose renew time is fresh, always including this replica.
func (c *Coordinator) liveMembers(ctx context.Context) ([]string, error) {
	sel := fmt.Sprintf("%s=%s,%s=%s", labelComponent, c.cfg.Component, labelRole, roleMember)
	list, err := c.client.CoordinationV1().Leases(c.cfg.Namespace).
		List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}
	set := map[string]bool{c.cfg.ReplicaID: true} // we know we're alive
	for i := range list.Items {
		l := list.Items[i]
		if l.Labels[labelRole] != roleMember || l.Labels[labelComponent] != c.cfg.Component {
			continue // guard against a client that doesn't honor the label selector
		}
		holder := deref(l.Spec.HolderIdentity)
		if holder == freeHolder {
			continue
		}
		if !c.expired(l.Spec.RenewTime) {
			set[holder] = true
			continue
		}
		// Stale for membership. If it's another replica's lease and far past its
		// validity, reclaim it so dead replicas' member leases don't accumulate.
		if holder != c.cfg.ReplicaID && c.memberGCEligible(l.Spec.RenewTime) {
			c.deleteLease(ctx, l.Name)
		}
	}
	members := make([]string, 0, len(set))
	for m := range set {
		members = append(members, m)
	}
	sort.Strings(members)
	return members, nil
}

// acquireOrRenew acquires shard i (creating or taking over a free/expired lease)
// or renews it if already held. It returns whether this replica holds it after.
func (c *Coordinator) acquireOrRenew(ctx context.Context, shard int) bool {
	name := c.shardLeaseName(shard)
	leases := c.client.CoordinationV1().Leases(c.cfg.Namespace)
	now := metav1.NewMicroTime(c.now())

	lease, err := leases.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, cerr := leases.Create(ctx, c.newLease(name, roleShard, c.cfg.ReplicaID, now), metav1.CreateOptions{})
		if cerr != nil {
			if !apierrors.IsAlreadyExists(cerr) {
				klog.ErrorS(cerr, "creating shard lease", "shard", shard)
			}
			return false // lost the create race; try next tick
		}
		return true
	}
	if err != nil {
		klog.ErrorS(err, "getting shard lease", "shard", shard)
		return false
	}

	holder := deref(lease.Spec.HolderIdentity)
	switch {
	case holder == c.cfg.ReplicaID:
		lease.Spec.RenewTime = &now
	case holder == freeHolder || c.expired(lease.Spec.RenewTime):
		lease.Spec.HolderIdentity = new(c.cfg.ReplicaID)
		lease.Spec.AcquireTime = &now
		lease.Spec.RenewTime = &now
		lease.Spec.LeaseTransitions = new(deref(lease.Spec.LeaseTransitions) + 1)
	default:
		return false // held by a live replica
	}

	if _, uerr := leases.Update(ctx, lease, metav1.UpdateOptions{}); uerr != nil {
		if !apierrors.IsConflict(uerr) {
			klog.ErrorS(uerr, "updating shard lease", "shard", shard)
		}
		return false // conflict: someone else won; try next tick
	}
	return true
}

// release frees shard i if this replica holds it, so another replica can take
// over without waiting for the lease to expire.
func (c *Coordinator) release(ctx context.Context, shard int) {
	name := c.shardLeaseName(shard)
	leases := c.client.CoordinationV1().Leases(c.cfg.Namespace)

	lease, err := leases.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return
	}
	if deref(lease.Spec.HolderIdentity) != c.cfg.ReplicaID {
		return // not ours (already taken over); leave it
	}
	now := metav1.NewMicroTime(c.now())
	lease.Spec.HolderIdentity = new(freeHolder)
	lease.Spec.RenewTime = &now
	if _, uerr := leases.Update(ctx, lease, metav1.UpdateOptions{}); uerr != nil && !apierrors.IsConflict(uerr) {
		klog.ErrorS(uerr, "releasing shard lease", "shard", shard)
	}
}

// releaseAll frees every currently-owned shard lease on shutdown.
func (c *Coordinator) releaseAll() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, shard := range c.OwnedShards() {
		c.release(ctx, shard)
	}
}

func (c *Coordinator) newLease(name, role, holder string, now metav1.MicroTime) *coordinationv1.Lease {
	dur := int32(c.cfg.LeaseDuration / time.Second)
	return &coordinationv1.Lease{
		Name:      name,
		Namespace: c.cfg.Namespace,
		Labels: map[string]string{
			labelComponent: c.cfg.Component,
			labelRole:      role,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       new(holder),
			LeaseDurationSeconds: new(dur),
			AcquireTime:          &now,
			RenewTime:            &now,
		},
	}
}

// expired reports whether a lease with the given renew time is past its
// validity window (or has never been renewed).
func (c *Coordinator) expired(renew *metav1.MicroTime) bool {
	if renew == nil {
		return true
	}
	return c.now().Sub(renew.Time) > c.cfg.LeaseDuration
}

// memberGCEligible reports whether a member lease is stale enough to reclaim —
// far beyond its validity, so only genuinely-dead replicas' leases are removed.
func (c *Coordinator) memberGCEligible(renew *metav1.MicroTime) bool {
	if renew == nil {
		return true
	}
	return c.now().Sub(renew.Time) > c.cfg.LeaseDuration*memberGCMultiplier
}

// deleteLease removes a lease by name, ignoring a not-found (already reclaimed by
// another replica).
func (c *Coordinator) deleteLease(ctx context.Context, name string) {
	err := c.client.CoordinationV1().Leases(c.cfg.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		klog.ErrorS(err, "reclaiming stale member lease", "lease", name)
	}
}

func (c *Coordinator) memberLeaseName() string {
	return fmt.Sprintf("%s-member-%s", c.cfg.Component, c.cfg.ReplicaID)
}

func (c *Coordinator) shardLeaseName(shard int) string {
	return fmt.Sprintf("%s-shard-%d", c.cfg.Component, shard)
}

func indexOf(sorted []string, v string) int {
	for i, s := range sorted {
		if s == v {
			return i
		}
	}
	return -1
}

func equalIntSet(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
