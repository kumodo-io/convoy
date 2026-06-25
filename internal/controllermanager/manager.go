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
	"k8s.io/client-go/tools/clientcmd"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	clustercontroller "github.com/kumodo-io/convoy/internal/controller/cluster"
	gitrepositorycontroller "github.com/kumodo-io/convoy/internal/controller/gitrepository"
)

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

	resolver := auth.NewSecretResolver(kube)
	// Registry of agentless auth types. cfg is the controller's own config, used
	// for the inCluster connection mode.
	clusterAuth := clusterconn.Registry{
		corev1alpha1.ClusterAuthTypeKubeconfig: auth.KubeconfigAuthenticator{},
	}
	connector := clusterconn.NewConnector(cfg, clusterAuth, resolver)
	prober := clustercontroller.NewProber(connector)
	clusterController := clustercontroller.NewController(client, clusterInformer, prober)

	// Shared in-process artifact store: source controllers publish fetched
	// content; the release controller consumes it.
	artifactStore := artifact.NewMemoryStore()

	// GitRepository controller: resolves public repos (auth "none") to a revision
	// and publishes the content to the artifact store.
	// The Git auth registry slots into the fetcher once a real git auth type lands.
	gitRepoInformer := factory.Source().V1alpha1().GitRepositories()
	gitFetcher := gitrepositorycontroller.NewGitFetcher(resolver)
	gitController := gitrepositorycontroller.NewController(client, gitRepoInformer, gitFetcher, artifactStore)

	factory.Start(ctx.Done())

	errGroup := errgroup.Group{}
	errGroup.Go(func() error { return clusterController.Run(ctx, o.Workers) })
	errGroup.Go(func() error { return gitController.Run(ctx, o.Workers) })

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
