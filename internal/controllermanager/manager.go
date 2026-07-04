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
	clustercontroller "github.com/kumodo-io/convoy/internal/controller/cluster"
	gitrepositorycontroller "github.com/kumodo-io/convoy/internal/controller/gitrepository"
	helmregistrycontroller "github.com/kumodo-io/convoy/internal/controller/helmregistry"
	ociregistrycontroller "github.com/kumodo-io/convoy/internal/controller/ociregistry"
	releasecontroller "github.com/kumodo-io/convoy/internal/controller/release"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/inventory"
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
}

// NewOptions returns Options with defaults.
func NewOptions() *Options {
	return &Options{
		Resync:  2 * time.Minute,
		Workers: 2,
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
	// from the cluster convoy runs in.
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	factory := informers.NewSharedInformerFactory(client, o.Resync)
	clusterInformer := factory.Core().V1alpha1().Clusters()
	// Release controller: fetches via the coordinator, renders, and applies to the
	// target cluster (reached through the same connector the cluster controller
	// probes with).
	releaseInformer := factory.Core().V1alpha1().Releases()

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
	clusterController := clustercontroller.NewController(client, clusterInformer, prober, releasesForCluster)

	// Content-addressed artifact store, shared by the fetch coordinator (producer)
	// and consumed via the coordinator's result.
	artifactStore := artifact.NewMemoryStore()

	// GitRepository controller: verifies repos are reachable/authenticated.
	// The Git auth registry slots into the fetcher once a real git auth type lands.
	gitRepoInformer := factory.Source().V1alpha1().GitRepositories()
	gitFetcher := gitrepositorycontroller.NewGitFetcher(resolver)
	gitController := gitrepositorycontroller.NewController(client, gitRepoInformer, gitFetcher)

	// HelmRegistry controller: verifies HTTP Helm repos are reachable.
	helmRegInformer := factory.Source().V1alpha1().HelmRegistries()
	helmFetcher := helmregistrycontroller.NewHelmFetcher(resolver)
	helmController := helmregistrycontroller.NewController(client, helmRegInformer, helmFetcher)

	// OCIRegistry controller: verifies OCI registries are reachable.
	ociRegInformer := factory.Source().V1alpha1().OCIRegistries()
	ociFetcher := ociregistrycontroller.NewOCIFetcher(resolver)
	ociController := ociregistrycontroller.NewController(client, ociRegInformer, ociFetcher)

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
	releaseController := releasecontroller.NewController(client, releaseInformer, clusterInformer.Lister(), coordinator, resolver, inventory.NewMemoryStore(), deployer)

	factory.Start(ctx.Done())

	errGroup := errgroup.Group{}
	errGroup.Go(func() error { return clusterController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return gitController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return helmController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return ociController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return releaseController.Run(ctx, o.Workers) })

	return errGroup.Wait()
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

	return cmd
}
