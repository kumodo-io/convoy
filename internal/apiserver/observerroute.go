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

package apiserver

import (
	"context"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	coordinationlisters "k8s.io/client-go/listers/coordination/v1"
	restclient "k8s.io/client-go/rest"

	"github.com/kumodo-io/convoy/internal/sharding"
)

// observerRoute finds the observer replica that owns a shard from the observer's
// sharding leases, so the observer subresources reach the replica that caches the
// Release.
type observerRoute struct {
	factory informers.SharedInformerFactory
	leases  coordinationlisters.LeaseNamespaceLister
}

// newObserverRoute watches only the observer's leases in namespace: a lease
// informer for the whole cluster would also receive every node heartbeat.
func newObserverRoute(cfg *restclient.Config, namespace string) (*observerRoute, error) {
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	factory := informers.NewSharedInformerFactoryWithOptions(client, 0,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.LabelSelector = sharding.ComponentSelector(sharding.ObserverComponent)
		}),
	)
	return &observerRoute{
		factory: factory,
		leases:  factory.Coordination().V1().Leases().Lister().Leases(namespace),
	}, nil
}

func (r *observerRoute) start(ctx context.Context) {
	r.factory.Start(ctx.Done())
}

func (r *observerRoute) resolve(shard string) string {
	i, err := strconv.Atoi(shard)
	if err != nil {
		return ""
	}
	return sharding.OwnerAddress(r.leases, sharding.ObserverComponent, i, time.Now())
}
