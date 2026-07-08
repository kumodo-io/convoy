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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	"github.com/kumodo-io/convoy/internal/observer"
)

// Options configures the observer.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file. Empty means in-cluster config.
	Kubeconfig string
	// Resync is how often informers re-deliver all objects, driving periodic
	// drift re-checks of otherwise-unchanged Releases.
	Resync time.Duration
	// Workers is the number of concurrent drift-check workers.
	Workers int
}

// NewOptions returns Options with defaults.
func NewOptions() *Options {
	return &Options{
		Resync:  2 * time.Minute,
		Workers: 2,
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
	gitRepoInformer := factory.Source().V1alpha1().GitRepositories()
	helmRegInformer := factory.Source().V1alpha1().HelmRegistries()
	ociRegInformer := factory.Source().V1alpha1().OCIRegistries()

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

	factory.Start(ctx.Done())
	return obs.Run(ctx, o.Workers)
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

	return cmd
}
