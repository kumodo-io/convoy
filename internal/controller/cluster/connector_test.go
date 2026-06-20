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

package cluster

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
)

type fakeAuthenticator struct {
	cfg *rest.Config
	err error
}

func (f fakeAuthenticator) RESTConfig(context.Context, *corev1alpha1.Cluster, auth.SecretResolver) (*rest.Config, error) {
	return f.cfg, f.err
}

func agentlessCluster(authType corev1alpha1.ClusterAuthType) *corev1alpha1.Cluster {
	return &corev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns"},
		Spec: corev1alpha1.ClusterSpec{
			ConnectionMode: corev1alpha1.ClusterConnectionModeAgentless,
			Agentless: &corev1alpha1.ClusterAgentlessModeSpec{
				Auth: corev1alpha1.ClusterAuthSpec{Type: authType},
			},
		},
	}
}

func TestConnector_InClusterReturnsOwnConfig(t *testing.T) {
	inCfg := &rest.Config{Host: "https://in-cluster"}
	c := NewConnector(inCfg, Registry{}, nil)

	got, err := c.Connect(context.Background(), clusterWithInClusterConnectionMode())
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if got != inCfg {
		t.Errorf("Connect = %v, want the in-cluster config %v", got, inCfg)
	}
}

func TestConnector_AgentlessDelegatesToAuthenticator(t *testing.T) {
	authCfg := &rest.Config{Host: "https://target"}
	c := NewConnector(&rest.Config{Host: "https://in-cluster"},
		Registry{corev1alpha1.ClusterAuthTypeKubeconfig: fakeAuthenticator{cfg: authCfg}}, nil)

	got, err := c.Connect(context.Background(), agentlessCluster(corev1alpha1.ClusterAuthTypeKubeconfig))
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if got.Host != authCfg.Host {
		t.Errorf("Connect host = %q, want %q", got.Host, authCfg.Host)
	}
}

func TestConnector_UnregisteredAuthTypeIsUnsupported(t *testing.T) {
	c := NewConnector(&rest.Config{}, Registry{}, nil)

	_, err := c.Connect(context.Background(), agentlessCluster(corev1alpha1.ClusterAuthTypeToken))
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestConnector_AuthenticatorErrorIsProbeFailed(t *testing.T) {
	c := NewConnector(&rest.Config{},
		Registry{corev1alpha1.ClusterAuthTypeKubeconfig: fakeAuthenticator{err: errors.New("boom")}}, nil)

	_, err := c.Connect(context.Background(), agentlessCluster(corev1alpha1.ClusterAuthTypeKubeconfig))
	if !errors.Is(err, ErrProbeFailed) {
		t.Errorf("err = %v, want ErrProbeFailed", err)
	}
}

func TestConnector_AgentlessWithoutBlockIsProbeFailed(t *testing.T) {
	cl := agentlessCluster(corev1alpha1.ClusterAuthTypeKubeconfig)
	cl.Spec.Agentless = nil
	c := NewConnector(&rest.Config{}, Registry{}, nil)

	_, err := c.Connect(context.Background(), cl)
	if !errors.Is(err, ErrProbeFailed) {
		t.Errorf("err = %v, want ErrProbeFailed", err)
	}
}

func TestConnector_AgentModeIsUnsupported(t *testing.T) {
	cl := &corev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns"},
		Spec:       corev1alpha1.ClusterSpec{ConnectionMode: corev1alpha1.ClusterConnectionModeAgent},
	}
	c := NewConnector(&rest.Config{}, Registry{}, nil)

	_, err := c.Connect(context.Background(), cl)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
