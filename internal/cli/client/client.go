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

package cli

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/api/generated/clientset/versioned"
)

// Client is the surface the commands need. It is small and interface-typed so the
// command logic can be unit-tested against a fake without a live apiserver.
type Client interface {
	// GetRelease returns the named Release.
	GetRelease(ctx context.Context, namespace, name string) (*corev1alpha1.Release, error)
	// Pin sets the Release's pin to renderRef via the /pin subresource.
	Pin(ctx context.Context, namespace, name, renderRef string) (*corev1alpha1.Release, error)
	// Unpin clears the Release's pin via the /pin subresource.
	Unpin(ctx context.Context, namespace, name string) (*corev1alpha1.Release, error)
	// Sync requests a reconcile via the /sync subresource.
	Sync(ctx context.Context, namespace, name string) (*corev1alpha1.Release, error)
	// Preview returns the rendered desired objects for a Release (multi-document
	// YAML) via the /preview subresource.
	Preview(ctx context.Context, namespace, name string) ([]byte, error)
	// Diff returns the structured per-object drift for a Release (JSON) via the
	// /diff subresource.
	Diff(ctx context.Context, namespace, name string) ([]byte, error)
}

// restClient talks to the convoy aggregated apiserver. The /pin and /sync
// subresources are custom, so they are driven through the group RESTClient rather
// than the generated typed client.
type restClient struct {
	clientset versioned.Interface
	rc        rest.Interface
}

var _ Client = (*restClient)(nil)

func (c *restClient) GetRelease(ctx context.Context, namespace, name string) (*corev1alpha1.Release, error) {
	return c.clientset.CoreV1alpha1().Releases(namespace).Get(ctx, name, metav1.GetOptions{})
}

// putPin GET-modify-PUTs the Release through the /pin subresource. The Updater
// there ignores everything but .pin, so sending the whole object is safe.
func (c *restClient) putPin(ctx context.Context, namespace, name string, pin *corev1alpha1.ReleasePin) (*corev1alpha1.Release, error) {
	rel, err := c.GetRelease(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	rel.Pin = pin
	result := &corev1alpha1.Release{}
	err = c.rc.Put().
		Namespace(namespace).
		Resource("releases").
		Name(name).
		SubResource("pin").
		Body(rel).
		Do(ctx).
		Into(result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *restClient) Pin(ctx context.Context, namespace, name, renderRef string) (*corev1alpha1.Release, error) {
	return c.putPin(ctx, namespace, name, &corev1alpha1.ReleasePin{RenderRef: renderRef})
}

func (c *restClient) Unpin(ctx context.Context, namespace, name string) (*corev1alpha1.Release, error) {
	return c.putPin(ctx, namespace, name, nil)
}

func (c *restClient) Sync(ctx context.Context, namespace, name string) (*corev1alpha1.Release, error) {
	result := &corev1alpha1.Release{}
	err := c.rc.Post().
		Namespace(namespace).
		Resource("releases").
		Name(name).
		SubResource("sync").
		Do(ctx).
		Into(result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Preview GETs the /preview subresource, which the apiserver proxies to the
// observer; the response body is the rendered manifests as a YAML stream.
func (c *restClient) Preview(ctx context.Context, namespace, name string) ([]byte, error) {
	return c.rc.Get().
		Namespace(namespace).
		Resource("releases").
		Name(name).
		SubResource("preview").
		Do(ctx).
		Raw()
}

// Diff GETs the /diff subresource (proxied to the observer); the response body
// is a diffresult.Result as JSON.
func (c *restClient) Diff(ctx context.Context, namespace, name string) ([]byte, error) {
	return c.rc.Get().
		Namespace(namespace).
		Resource("releases").
		Name(name).
		SubResource("diff").
		Do(ctx).
		Raw()
}

// ConfigFlags hold the standard kubeconfig-selection flags and build a Client.
type ConfigFlags struct {
	Kubeconfig string
	Context    string
	Namespace  string
}

// clientConfig assembles a deferred-loading client config from the flags,
// honoring the usual KUBECONFIG discovery when --kubeconfig is unset.
func (f *ConfigFlags) clientConfig() clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if f.Kubeconfig != "" {
		loadingRules.ExplicitPath = f.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if f.Context != "" {
		overrides.CurrentContext = f.Context
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
}

// Client builds a Client and resolves the target namespace (--namespace wins,
// otherwise the kubeconfig context's namespace).
func (f *ConfigFlags) Client() (Client, string, error) {
	cc := f.clientConfig()

	namespace := f.Namespace
	if namespace == "" {
		ns, _, err := cc.Namespace()
		if err != nil {
			return nil, "", err
		}
		namespace = ns
	}

	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, "", err
	}
	clientset, err := versioned.NewForConfig(restCfg)
	if err != nil {
		return nil, "", err
	}
	return &restClient{clientset: clientset, rc: clientset.CoreV1alpha1().RESTClient()}, namespace, nil
}
