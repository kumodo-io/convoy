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

// Package observermanager wires and runs the convoy observer: a standalone
// component that detects drift between each Release's desired render and the live
// target-cluster state. It renders independently (its own fetch coordinator) and
// reaches target clusters through the shared connector; it never applies.
package observermanager

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	gitrepositorycontroller "github.com/kumodo-io/convoy/internal/controller/gitrepository"
	helmregistrycontroller "github.com/kumodo-io/convoy/internal/controller/helmregistry"
	ociregistrycontroller "github.com/kumodo-io/convoy/internal/controller/ociregistry"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/inventory"
	"github.com/kumodo-io/convoy/internal/observer"
	"github.com/kumodo-io/convoy/internal/scopedinformers"
	"github.com/kumodo-io/convoy/internal/sharding"
)

const (
	// shardingComponent scopes the observer's sharding leases (independent from
	// the controller-manager's).
	shardingComponent = "convoy-obs"
	// defaultLeaseNamespace holds sharding leases when neither the flag nor
	// $POD_NAMESPACE is set.
	defaultLeaseNamespace = "convoy-system"
)

// releaseInventoryKey extracts the namespace/name cache key from a
// ReleaseInventory informer object (handling the tombstone on delete). The key
// equals the Release key, since the two share a name and namespace.
func releaseInventoryKey(obj any) (string, bool) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return "", false
	}
	return key, true
}

// Options configures the observer.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file. Empty means in-cluster config.
	Kubeconfig string
	// Resync is how often informers re-deliver all objects, driving periodic
	// drift re-checks of otherwise-unchanged Releases.
	Resync time.Duration
	// Workers is the number of concurrent drift-check workers.
	Workers int
	// ServeAddr is the address the on-demand HTTP server (/preview, later /diff)
	// listens on. The apiserver proxies the Release subresources here.
	ServeAddr string
	// Shards is the fixed shard count S. Must match across the fleet (and equal
	// the controller-manager's, though the two shard independently). Work is
	// partitioned by target cluster.
	Shards int
	// ShardScopedCache enables shard-scoped informers for Release/ReleaseInventory/
	// Cluster: each replica caches only the objects for the shards it owns, so
	// per-replica memory scales with replica count. Default off (full cache).
	// Requires the shard label on those objects.
	ShardScopedCache bool
	// ReplicaID is this replica's shard-lease holder identity. Empty resolves
	// from $POD_NAME, then the hostname.
	ReplicaID string
	// LeaseNamespace is where sharding Leases live. Empty resolves from
	// $POD_NAMESPACE, then "convoy-system".
	LeaseNamespace string
	// LeaseDuration is how long a held shard lease is valid without renewal.
	LeaseDuration time.Duration
	// RenewInterval is how often the coordinator renews leases and re-evaluates
	// ownership.
	RenewInterval time.Duration
}

// NewOptions returns Options with defaults.
func NewOptions() *Options {
	return &Options{
		Resync:        2 * time.Minute,
		Workers:       2,
		ServeAddr:     ":8080",
		Shards:        32,
		LeaseDuration: 30 * time.Second,
		RenewInterval: 10 * time.Second,
	}
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

// Run builds the clients, coordinator, connector, and observer, then blocks until
// ctx is cancelled.
func (o *Options) Run(ctx context.Context) error {
	cfg, err := buildConfig(o.Kubeconfig)
	if err != nil {
		return err
	}
	client, err := clientset.NewForConfig(cfg)
	if err != nil {
		return err
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	// Sharding: partition Releases by target cluster across observer replicas via
	// leases, independently of the controller-manager.
	coord := sharding.NewCoordinator(kube, sharding.Config{
		Component:     shardingComponent,
		ReplicaID:     resolveReplicaID(o.ReplicaID),
		Namespace:     resolveLeaseNamespace(o.LeaseNamespace),
		Shards:        o.Shards,
		LeaseDuration: o.LeaseDuration,
		RenewInterval: o.RenewInterval,
	})
	sharder := coord.Sharder()
	owns := func(namespace, name string) bool { return coord.Owns(sharder.Shard(namespace, name)) }

	// The plain factory backs the full caches: sources (a Release's source may be in
	// any shard) and the health rule sets (few, small, selected by label).
	factory := informers.NewSharedInformerFactory(client, o.Resync)
	// The per-Release caches (Release, ReleaseInventory, Cluster) are shard-scoped so
	// memory scales with replica count; the scoped set rebuilds on ownership change.
	scoped := scopedinformers.New(client, o.Resync, o.ShardScopedCache)
	releaseInformer := scoped.Releases()
	clusterInformer := scoped.Clusters()
	// ReleaseInventory carries the durable per-resource health the health loop
	// writes (and the controller rolls up for the sync-wave gate); the lister
	// backs both the anchor reads and the /health subresource.
	releaseInventoryInformer := scoped.ReleaseInventories()
	gitRepoInformer := factory.Source().V1alpha1().GitRepositories()
	helmRegInformer := factory.Source().V1alpha1().HelmRegistries()
	ociRegInformer := factory.Source().V1alpha1().OCIRegistries()
	// Health rule sets feed the health loop; instantiating the informers here
	// ensures the factory starts and syncs their caches before assessment runs.
	globalRuleSetInformer := factory.Extensions().V1alpha1().GlobalHealthRuleSets()
	ruleSetInformer := factory.Extensions().V1alpha1().HealthRuleSets()

	resolver := auth.NewSecretResolver(kube)
	clusterAuth := clusterconn.Registry{
		corev1alpha1.ClusterAuthTypeKubeconfig: auth.KubeconfigAuthenticator{},
	}
	connector := clusterconn.NewConnector(cfg, clusterAuth, resolver)

	// The observer fetches independently (separate process, its own store). The
	// fetchers verify+fetch source content; the coordinator resolves per Release.
	gitFetcher := gitrepositorycontroller.NewGitFetcher(resolver)
	helmFetcher := helmregistrycontroller.NewHelmFetcher(resolver)
	ociFetcher := ociregistrycontroller.NewOCIFetcher(resolver)
	coordinator := fetch.NewCoordinator(fetch.Deps{
		Store:          artifact.NewMemoryStore(),
		GitRepos:       gitRepoInformer.Lister(),
		GitFetcher:     gitFetcher,
		HelmRegistries: helmRegInformer.Lister(),
		HelmFetcher:    helmFetcher,
		OCIRegistries:  ociRegInformer.Lister(),
		OCIFetcher:     ociFetcher,
	})

	obs := observer.NewController(client, releaseInformer, clusterInformer.Lister(), coordinator, resolver, connector, owns)
	healthCtl := observer.NewHealthController(client, releaseInformer, clusterInformer.Lister(), connector,
		globalRuleSetInformer.Lister(), ruleSetInformer.Lister(), releaseInventoryInformer.Lister(), owns)
	// The drift loop hosts the HTTP server; wire the /health lookup to the health
	// loop so /preview, /diff, and /health are served from one endpoint.
	obs.SetHealthResults(healthCtl.HealthResult)

	// Wake the health loop when a Release's ReleaseInventory changes in a way the
	// observer must react to: a resource was applied/pruned (membership), or a
	// lifecycle hook was (re-)armed (workload created). The RI is named after its
	// Release and shares its namespace, so its cache key is the Release key. This is
	// what lets the observer assess a resource / poll a hook the moment it appears,
	// rather than on the poll interval. The observer's own writes (health, hook
	// status) are filtered out so they don't self-trigger.
	if _, err := releaseInventoryInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if key, ok := releaseInventoryKey(obj); ok {
				healthCtl.EnqueueAfter(key, 0)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldInv, ok1 := oldObj.(*corev1alpha1.ReleaseInventory)
			newInv, ok2 := newObj.(*corev1alpha1.ReleaseInventory)
			if ok1 && ok2 && (inventory.MembershipChanged(oldInv, newInv) || inventory.HooksArmChanged(oldInv, newInv)) {
				if key, ok := releaseInventoryKey(newObj); ok {
					healthCtl.EnqueueAfter(key, 0)
				}
			}
		},
	}); err != nil {
		return err
	}

	// On a shard reassignment, re-drive the Releases this replica now owns through
	// both loops (informer ADDs cover the initial set; this handles rebalances).
	coord.OnChange(func() {
		// Re-point the shard-scoped caches to the newly-owned shards before enqueuing.
		if err := scoped.Rebuild(ctx, coord.OwnedShards()); err != nil {
			klog.ErrorS(err, "rebuilding shard-scoped informers on ownership change")
		}
		rels, _ := releaseInformer.Lister().List(labels.Everything())
		keys := ownedReleaseKeys(rels, owns)
		obs.EnqueueKeys(keys)
		healthCtl.EnqueueKeys(keys)
	})

	// Prime shard ownership before informers deliver their initial events.
	coord.Prime(ctx)

	// Build the shard-scoped informers for the owned shards and sync their caches
	// (a no-op if Prime's OnChange already built them; also covers the zero-owned
	// case). The observer reads the Cluster and (below) source caches during each
	// drift pass, so these must be synced before reconciling.
	if err := scoped.Rebuild(ctx, coord.OwnedShards()); err != nil {
		return err
	}

	factory.Start(ctx.Done())
	// The shard-scoped Release/Cluster/ReleaseInventory caches were synced by
	// Rebuild above. Each drift pass also reads the three source caches (via the
	// coordinator's listers), which live on the plain factory, so wait for every
	// started informer here first — otherwise the observer would begin reconciling
	// before those caches populate and report spurious "source not found" for
	// objects that exist, until the caches catch up.
	factory.WaitForCacheSync(ctx.Done())

	// Run the drift loop and the on-demand HTTP server together; if either returns,
	// cancel the other and surface the error.
	group, gctx := errgroup.WithContext(ctx)
	group.Go(func() error { return coord.Run(gctx) })
	group.Go(func() error { return obs.Run(gctx, o.Workers) })
	group.Go(func() error { return healthCtl.Run(gctx, o.Workers) })
	group.Go(func() error { return obs.Serve(gctx, o.ServeAddr) })
	return group.Wait()
}

// resolveReplicaID returns the configured replica ID, or falls back to
// $POD_NAME, then the hostname.
func resolveReplicaID(configured string) string {
	if configured != "" {
		return configured
	}
	if pn := os.Getenv("POD_NAME"); pn != "" {
		return pn
	}
	host, _ := os.Hostname()
	return host
}

// resolveLeaseNamespace returns the configured lease namespace, or falls back to
// $POD_NAMESPACE, then defaultLeaseNamespace.
func resolveLeaseNamespace(configured string) string {
	if configured != "" {
		return configured
	}
	if podNS := os.Getenv("POD_NAMESPACE"); podNS != "" {
		return podNS
	}
	return defaultLeaseNamespace
}

// ownedReleaseKeys returns the namespace/name keys of Releases whose target
// cluster this replica owns.
func ownedReleaseKeys(rels []*corev1alpha1.Release, owns func(clusterNamespace, clusterName string) bool) []string {
	keys := make([]string, 0, len(rels))
	for _, rel := range rels {
		cns := rel.Spec.ClusterRef.Namespace
		if cns == "" {
			cns = rel.Namespace
		}
		if owns(cns, rel.Spec.ClusterRef.Name) {
			keys = append(keys, rel.Namespace+"/"+rel.Name)
		}
	}
	return keys
}

// NewCommand builds the cobra command that starts the observer.
func NewCommand(ctx context.Context) *cobra.Command {
	o := NewOptions()
	cmd := &cobra.Command{
		Use:   "convoy-observer",
		Short: "Run the convoy drift observer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.Run(cmd.Context())
		},
	}
	cmd.SetContext(ctx)

	flags := cmd.Flags()
	flags.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig, "Path to a kubeconfig file. Empty uses in-cluster config.")
	flags.DurationVar(&o.Resync, "resync", o.Resync, "Informer resync period; drives periodic drift re-checks.")
	flags.IntVar(&o.Workers, "workers", o.Workers, "Number of concurrent drift-check workers.")
	flags.StringVar(&o.ServeAddr, "serve-addr", o.ServeAddr, "Address for the on-demand HTTP server (/preview) the apiserver proxies to.")
	flags.IntVar(&o.Shards, "shards", o.Shards, "Fixed shard count S; work is partitioned by target cluster. Must match across the fleet.")
	flags.BoolVar(&o.ShardScopedCache, "shard-scoped-cache", o.ShardScopedCache, "Cache only the owned shards' Releases/ReleaseInventories/Clusters so memory scales with replica count. Requires the shard label on those objects.")
	flags.StringVar(&o.ReplicaID, "replica-id", o.ReplicaID, "Unique shard-lease holder identity. Empty uses $POD_NAME, then the hostname.")
	flags.StringVar(&o.LeaseNamespace, "lease-namespace", o.LeaseNamespace, "Namespace for sharding leases. Empty uses $POD_NAMESPACE, then \"convoy-system\".")
	flags.DurationVar(&o.LeaseDuration, "lease-duration", o.LeaseDuration, "How long a held shard lease is valid without renewal.")
	flags.DurationVar(&o.RenewInterval, "lease-renew-interval", o.RenewInterval, "How often shard leases are renewed and ownership re-evaluated.")

	return cmd
}
