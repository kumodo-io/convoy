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
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

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
}

// NewOptions returns Options with defaults.
func NewOptions() *Options {
	return &Options{
		Resync:    2 * time.Minute,
		Workers:   2,
		ServeAddr: ":8080",
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

	factory := informers.NewSharedInformerFactory(client, o.Resync)
	releaseInformer := factory.Core().V1alpha1().Releases()
	clusterInformer := factory.Core().V1alpha1().Clusters()
	// ReleaseInventory carries the durable per-resource health the health loop
	// writes (and the controller rolls up for the sync-wave gate); the lister
	// backs both the anchor reads and the /health subresource.
	releaseInventoryInformer := factory.Core().V1alpha1().ReleaseInventories()
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

	obs := observer.NewController(client, releaseInformer, clusterInformer.Lister(), coordinator, resolver, connector)
	healthCtl := observer.NewHealthController(client, releaseInformer, clusterInformer.Lister(), connector,
		globalRuleSetInformer.Lister(), ruleSetInformer.Lister(), releaseInventoryInformer.Lister())
	// The drift loop hosts the HTTP server; wire the /health lookup to the health
	// loop so /preview, /diff, and /health are served from one endpoint.
	obs.SetHealthResults(healthCtl.HealthResult)

	// Wake the health loop when a Release's ReleaseInventory membership changes — a
	// resource was just applied (or pruned). The RI is named after its Release and
	// shares its namespace, so its cache key is the Release key. This is what lets
	// the observer assess a resource the moment it appears, rather than on the poll
	// interval. Health-only updates (the observer's own writes) are filtered out.
	if _, err := releaseInventoryInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if key, ok := releaseInventoryKey(obj); ok {
				healthCtl.EnqueueAfter(key, 0)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldInv, ok1 := oldObj.(*corev1alpha1.ReleaseInventory)
			newInv, ok2 := newObj.(*corev1alpha1.ReleaseInventory)
			if ok1 && ok2 && inventory.MembershipChanged(oldInv, newInv) {
				if key, ok := releaseInventoryKey(newObj); ok {
					healthCtl.EnqueueAfter(key, 0)
				}
			}
		},
	}); err != nil {
		return err
	}

	factory.Start(ctx.Done())
	// The observer only *watches* Releases, so base.Controller.Run waits for the
	// Release cache alone. But each drift pass also reads the Cluster and the three
	// source caches (via the coordinator's listers), so wait for every started
	// informer here first — otherwise the observer would begin reconciling before
	// those caches populate and report spurious "source/cluster not found" for
	// objects that exist, until the caches catch up.
	factory.WaitForCacheSync(ctx.Done())

	// Run the drift loop and the on-demand HTTP server together; if either returns,
	// cancel the other and surface the error.
	group, gctx := errgroup.WithContext(ctx)
	group.Go(func() error { return obs.Run(gctx, o.Workers) })
	group.Go(func() error { return healthCtl.Run(gctx, o.Workers) })
	group.Go(func() error { return obs.Serve(gctx, o.ServeAddr) })
	return group.Wait()
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

	return cmd
}
