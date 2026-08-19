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

// Package controllermanager wires and runs the convoy controllers against the
// convoy API (reached through the host kube-apiserver's aggregation layer).
package controllermanager

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
	clustercontroller "github.com/kumodo-io/convoy/internal/controller/cluster"
	gitrepositorycontroller "github.com/kumodo-io/convoy/internal/controller/gitrepository"
	helmregistrycontroller "github.com/kumodo-io/convoy/internal/controller/helmregistry"
	ociregistrycontroller "github.com/kumodo-io/convoy/internal/controller/ociregistry"
	releasecontroller "github.com/kumodo-io/convoy/internal/controller/release"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/inventory"
	"github.com/kumodo-io/convoy/internal/renderstore"
	"github.com/kumodo-io/convoy/internal/scopedinformers"
	"github.com/kumodo-io/convoy/internal/sharding"
	"github.com/kumodo-io/convoy/internal/waves"
)

const (
	// shardingComponent scopes this manager's sharding leases.
	shardingComponent = "convoy-cm"
	// defaultLeaseNamespace holds sharding leases when neither the flag nor
	// $POD_NAMESPACE is set.
	defaultLeaseNamespace = "convoy-system"
)

// releaseByClusterIndex is the informer index keying Releases by the
// namespace/name of the cluster they target.
const releaseByClusterIndex = "releaseByCluster"

// releaseClusterIndexKeys is the index func for releaseByClusterIndex. It keys a
// Release by "<clusterNamespace>/<clusterName>", defaulting the cluster namespace
// to the Release's own namespace (matching how the release controller resolves it).
func releaseClusterIndexKeys(obj any) ([]string, error) {
	rel, ok := obj.(*corev1alpha1.Release)
	if !ok {
		return nil, nil
	}
	ns := rel.Spec.ClusterRef.Namespace
	if ns == "" {
		ns = rel.Namespace
	}
	return []string{ns + "/" + rel.Spec.ClusterRef.Name}, nil
}

// Options configures the controller manager.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file. Empty means in-cluster config.
	Kubeconfig string
	// Resync is how often informers re-deliver all objects, which drives
	// periodic re-probing of otherwise-unchanged Clusters.
	Resync time.Duration
	// Workers is the number of concurrent reconcile workers per controller.
	Workers int
	// Shards is the fixed shard count S. Must match across the fleet. Work is
	// partitioned by target cluster into S shards, owned via leases.
	Shards int
	// ShardScopedCache enables shard-scoped informers for Release/ReleaseInventory/
	// Cluster: each replica caches only the objects for the shards it owns, so
	// per-replica memory scales with replica count rather than total object count.
	// Default off (full cache — behaviourally identical to before). Requires the
	// shard label on those objects. See docs/architecture/05-shard-scoped-caches.md.
	ShardScopedCache bool
	// RenderStoreURL is the gocloud blob URL of the durable render store (e.g.
	// "s3://bucket", "gs://bucket", "file:///var/lib/convoy/renders"). Empty
	// disables persistence: the controller renders and applies as before, without
	// a durable render history. See docs/architecture/03-persistent-storage.md.
	RenderStoreURL string
	// ReplicaID is this replica's unique shard-lease holder identity. Empty
	// resolves from $POD_NAME, then the hostname.
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
		Shards:        32,
		LeaseDuration: 30 * time.Second,
		RenewInterval: 10 * time.Second,
	}
}

// buildConfig returns a rest.Config from --kubeconfig, or in-cluster config.
func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

// Run builds the clients, controllers, and informers, then blocks until ctx is
// cancelled.
func (o *Options) Run(ctx context.Context) error {
	cfg, err := buildConfig(o.Kubeconfig)
	if err != nil {
		return err
	}

	client, err := clientset.NewForConfig(cfg)
	if err != nil {
		return err
	}

	// kube is used to resolve referenced Secrets/ConfigMaps (e.g. kubeconfigs)
	// from the cluster convoy runs in, and to maintain sharding leases.
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	// Sharding: partition work by target cluster across replicas via leases.
	coord := sharding.NewCoordinator(kube, sharding.Config{
		Component:     shardingComponent,
		ReplicaID:     resolveReplicaID(o.ReplicaID),
		Namespace:     resolveLeaseNamespace(o.LeaseNamespace),
		Shards:        o.Shards,
		LeaseDuration: o.LeaseDuration,
		RenewInterval: o.RenewInterval,
	})
	sharder := coord.Sharder()
	// owns reports whether this replica owns the shard for a namespace/name. For
	// Cluster and source objects that identity is their own key; for a Release it
	// is the target cluster's key.
	owns := func(namespace, name string) bool { return coord.Owns(sharder.Shard(namespace, name)) }

	// The plain factory backs the low-cardinality, shared caches (sources); those
	// stay full because a Release's source may be in any shard.
	factory := informers.NewSharedInformerFactory(client, o.Resync)
	// The high-cardinality per-Release caches (Release, ReleaseInventory, Cluster)
	// are shard-scoped: each replica caches only its owned shards, so memory scales
	// with replica count. The scoped set hands out stable typed facades and rebuilds
	// its filtered factory whenever ownership changes (see scopedinformers).
	scoped := scopedinformers.New(client, o.Resync, o.ShardScopedCache)
	clusterInformer := scoped.Clusters()
	// Release controller: fetches via the coordinator, renders, and applies to the
	// target cluster (reached through the same connector the cluster controller
	// probes with).
	releaseInformer := scoped.Releases()
	// ReleaseInventory records each Release's applied resource set durably, so the
	// controller can prune after a restart; the informer feeds Store reads.
	releaseInventoryInformer := scoped.ReleaseInventories()

	resolver := auth.NewSecretResolver(kube)
	// Registry of agentless auth types. cfg is the controller's own config, used
	// for the inCluster connection mode.
	clusterAuth := clusterconn.Registry{
		corev1alpha1.ClusterAuthTypeKubeconfig: auth.KubeconfigAuthenticator{},
	}
	connector := clusterconn.NewConnector(cfg, clusterAuth, resolver)
	prober := clustercontroller.NewProber(connector)
	releasesForCluster := func(clusterNs, clusterName string) ([]*corev1alpha1.Release, error) {
		objs, err := releaseInformer.Informer().GetIndexer().
			ByIndex(releaseByClusterIndex, clusterNs+"/"+clusterName)
		if err != nil {
			return nil, err
		}
		out := make([]*corev1alpha1.Release, 0, len(objs))
		for _, o := range objs {
			if rel, ok := o.(*corev1alpha1.Release); ok {
				out = append(out, rel)
			}
		}
		return out, nil
	}
	clusterController := clustercontroller.NewController(client, clusterInformer, prober, releasesForCluster, base.WithShardGate(owns))

	// Content-addressed artifact store, shared by the fetch coordinator (producer)
	// and consumed via the coordinator's result.
	artifactStore := artifact.NewMemoryStore()

	// GitRepository controller: verifies repos are reachable/authenticated.
	// The Git auth registry slots into the fetcher once a real git auth type lands.
	gitRepoInformer := factory.Source().V1alpha1().GitRepositories()
	gitFetcher := gitrepositorycontroller.NewGitFetcher(resolver)
	gitController := gitrepositorycontroller.NewController(client, gitRepoInformer, gitFetcher, base.WithShardGate(owns))

	// HelmRegistry controller: verifies HTTP Helm repos are reachable.
	helmRegInformer := factory.Source().V1alpha1().HelmRegistries()
	helmFetcher := helmregistrycontroller.NewHelmFetcher(resolver)
	helmController := helmregistrycontroller.NewController(client, helmRegInformer, helmFetcher, base.WithShardGate(owns))

	// OCIRegistry controller: verifies OCI registries are reachable.
	ociRegInformer := factory.Source().V1alpha1().OCIRegistries()
	ociFetcher := ociregistrycontroller.NewOCIFetcher(resolver)
	ociController := ociregistrycontroller.NewController(client, ociRegInformer, ociFetcher, base.WithShardGate(owns))

	// Fetch coordinator: resolves a Release's source coordinate and fetches
	// content on demand into the artifact store.
	coordinator := fetch.NewCoordinator(fetch.Deps{
		Store:          artifactStore,
		GitRepos:       gitRepoInformer.Lister(),
		GitFetcher:     gitFetcher,
		HelmRegistries: helmRegInformer.Lister(),
		HelmFetcher:    helmFetcher,
		OCIRegistries:  ociRegInformer.Lister(),
		OCIFetcher:     ociFetcher,
	})

	// Index Releases by the cluster they target, so cluster-driven fan-out (e.g.
	// re-enqueue/prune on cluster change) can look them up from the cache.
	if err := releaseInformer.Informer().AddIndexers(cache.Indexers{
		releaseByClusterIndex: releaseClusterIndexKeys,
	}); err != nil {
		return err
	}
	deployer := releasecontroller.NewDeployer(connector)
	hookRunner := releasecontroller.NewHookRunner(connector)
	inventoryStore := inventory.NewAPIStore(client, releaseInventoryInformer.Lister(), releaseInformer.Lister())

	// Durable render store (optional). When --render-store-url is set, each applied
	// render is persisted (content-addressed) and recorded in an ordered deploy log,
	// backing history/rollback/diff; empty disables persistence.
	var renderStore *renderstore.RenderStore
	if o.RenderStoreURL != "" {
		bs, closeStore, err := blobstore.Open(ctx, o.RenderStoreURL)
		if err != nil {
			return fmt.Errorf("opening render store: %w", err)
		}
		defer func() { _ = closeStore() }()
		renderStore = renderstore.New(bs)
	}
	releaseController := releasecontroller.NewController(client, releaseInformer, clusterInformer.Lister(), coordinator, resolver, inventoryStore, renderStore, deployer, hookRunner, releasesForCluster, owns)

	// Axis B fan-out: when a Release's Applied/Healthy signal flips, re-enqueue its
	// higher-wave successors on the same cluster so they re-evaluate the cluster-wave
	// gate immediately instead of on their poll interval. It is only a wake — the
	// gate itself decides whether a successor proceeds.
	if _, err := releaseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj any) {
			oldRel, ok1 := oldObj.(*corev1alpha1.Release)
			newRel, ok2 := newObj.(*corev1alpha1.Release)
			if !ok1 || !ok2 || !releasecontroller.ClusterWaveSignalChanged(oldRel, newRel) {
				return
			}
			clusterNs := newRel.Spec.ClusterRef.Namespace
			if clusterNs == "" {
				clusterNs = newRel.Namespace
			}
			siblings, err := releasesForCluster(clusterNs, newRel.Spec.ClusterRef.Name)
			if err != nil {
				return
			}
			changedWave := waves.ClusterWaveOf(newRel)
			for _, s := range siblings {
				if waves.ClusterWaveOf(s) > changedWave {
					releaseController.EnqueueAfter(s.Namespace+"/"+s.Name, 0)
				}
			}
		},
	}); err != nil {
		return err
	}

	// Wake the release controller when the observer writes per-resource health to a
	// ReleaseInventory (State / AssessedRenderRef changes). The RI shares its Release's
	// namespace/name, so its key IS the Release key. Without this the controller only
	// re-reads health on its poll interval, so the failurePolicy rollout gate would
	// confirm a render healthy up to a whole poll late — which can cross the deadline
	// and wrongly roll back a workload that became healthy in time. Membership changes
	// are ignored here (the observer's health loop handles the "resource appeared" poke).
	if _, err := releaseInventoryInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj any) {
			oldInv, ok1 := oldObj.(*corev1alpha1.ReleaseInventory)
			newInv, ok2 := newObj.(*corev1alpha1.ReleaseInventory)
			if ok1 && ok2 && inventory.HealthChanged(oldInv, newInv) {
				releaseController.EnqueueAfter(newInv.Namespace+"/"+newInv.Name, 0)
			}
		},
	}); err != nil {
		return err
	}

	// On a shard reassignment, re-drive the work this replica now owns. Informer
	// ADDs cover the initial set; this handles rebalances (a replica joining or
	// leaving) without waiting for the next informer resync.
	coord.OnChange(func() {
		// Re-point the shard-scoped caches to the newly-owned shards first, so the
		// enqueue below (and subsequent reconciles) read the correct cache.
		//
		// This runs inline on the coordinator's tick goroutine and blocks on the new
		// informers' cache sync, so it delays the next member-lease renewal by the
		// sync duration. Acceptable because ownership changes are rare (a replica
		// joining/leaving) and the delay is bounded well under the lease-duration
		// slack; a wedged apiserver here would stall renewal, but it would equally
		// stall all reconciliation, and the compute-time gate keeps correctness.
		if err := scoped.Rebuild(ctx, coord.OwnedShards()); err != nil {
			klog.ErrorS(err, "rebuilding shard-scoped informers on ownership change")
		}
		rels, _ := releaseInformer.Lister().List(labels.Everything())
		releaseController.EnqueueKeys(ownedReleaseKeys(rels, owns))
		clusters, _ := clusterInformer.Lister().List(labels.Everything())
		clusterController.EnqueueKeys(ownedSelfKeys(clusters, owns))
		gits, _ := gitRepoInformer.Lister().List(labels.Everything())
		gitController.EnqueueKeys(ownedSelfKeys(gits, owns))
		helms, _ := helmRegInformer.Lister().List(labels.Everything())
		helmController.EnqueueKeys(ownedSelfKeys(helms, owns))
		ocis, _ := ociRegInformer.Lister().List(labels.Everything())
		ociController.EnqueueKeys(ownedSelfKeys(ocis, owns))
	})

	// Prime shard ownership before informers deliver their initial events, so the
	// enqueue filters see correct ownership from the first ADD.
	coord.Prime(ctx)

	// Build the shard-scoped informers for the owned shards and wait for their
	// caches to sync. Prime's OnChange above may already have built them; Rebuild
	// is a no-op when the selector is unchanged, and this call also covers the
	// zero-owned edge case (where Prime fires no change). It syncs the Release,
	// Cluster, and ReleaseInventory caches together — including the RI cache the
	// release controller reads during the delete/prune path (it watches Releases,
	// not the RI, so nothing else waits for it), preventing an early delete from
	// reading an empty inventory and orphaning resources.
	if err := scoped.Rebuild(ctx, coord.OwnedShards()); err != nil {
		return fmt.Errorf("building shard-scoped informers: %w", err)
	}

	factory.Start(ctx.Done())

	errGroup := errgroup.Group{}
	errGroup.Go(func() error { return coord.Run(ctx) })
	errGroup.Go(func() error { return clusterController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return gitController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return helmController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return ociController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return releaseController.Run(ctx, o.Workers) })

	return errGroup.Wait()
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

// ownedSelfKeys returns the namespace/name keys of objects this replica owns by
// their own identity (Cluster and the source kinds).
func ownedSelfKeys[T metav1.Object](items []T, owns func(namespace, name string) bool) []string {
	keys := make([]string, 0, len(items))
	for _, it := range items {
		if owns(it.GetNamespace(), it.GetName()) {
			keys = append(keys, it.GetNamespace()+"/"+it.GetName())
		}
	}
	return keys
}

// NewCommand builds the cobra command that starts the controller manager.
func NewCommand(ctx context.Context) *cobra.Command {
	o := NewOptions()

	cmd := &cobra.Command{
		Use:   "convoy-controller-manager",
		Short: "Run the convoy controllers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.Run(cmd.Context())
		},
	}
	cmd.SetContext(ctx)

	flags := cmd.Flags()
	flags.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig, "Path to a kubeconfig file. Empty uses in-cluster config.")
	flags.DurationVar(&o.Resync, "resync", o.Resync, "Informer resync period; drives periodic re-probing of Clusters.")
	flags.IntVar(&o.Workers, "workers", o.Workers, "Number of concurrent reconcile workers.")
	flags.IntVar(&o.Shards, "shards", o.Shards, "Fixed shard count S; work is partitioned by target cluster into S shards. Must match across the fleet.")
	flags.BoolVar(&o.ShardScopedCache, "shard-scoped-cache", o.ShardScopedCache, "Cache only the owned shards' Releases/ReleaseInventories/Clusters so memory scales with replica count. Requires the shard label on those objects.")
	flags.StringVar(&o.RenderStoreURL, "render-store-url", o.RenderStoreURL, "gocloud blob URL for durable render storage (e.g. s3://bucket, gs://bucket, file:///path). Empty disables render persistence/history.")
	flags.StringVar(&o.ReplicaID, "replica-id", o.ReplicaID, "Unique shard-lease holder identity. Empty uses $POD_NAME, then the hostname.")
	flags.StringVar(&o.LeaseNamespace, "lease-namespace", o.LeaseNamespace, "Namespace for sharding leases. Empty uses $POD_NAMESPACE, then \"convoy-system\".")
	flags.DurationVar(&o.LeaseDuration, "lease-duration", o.LeaseDuration, "How long a held shard lease is valid without renewal.")
	flags.DurationVar(&o.RenewInterval, "lease-renew-interval", o.RenewInterval, "How often shard leases are renewed and ownership re-evaluated.")

	return cmd
}
