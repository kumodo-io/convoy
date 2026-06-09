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

// Package registrytest provides shared helpers for testing convoy's REST
// storage against an in-process etcd server.
package registrytest

import (
	"io"
	"testing"

	"google.golang.org/grpc/grpclog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/registry/generic"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"
)

func init() {
	// The embedded etcd test server and its client are torn down at the end of
	// every test, which makes gRPC log a flurry of benign "connection refused"
	// warnings for in-flight connections. Discard them so test output only shows
	// genuine failures.
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, io.Discard))
}

// NewEtcdStorage builds a scheme (via the group's install func) and an
// etcd-backed generic.RESTOptions pointed at a fresh in-process etcd test
// server. Objects are encoded as encodeGV. Callers must Terminate the returned
// server when done.
func NewEtcdStorage(
	t *testing.T,
	install func(*runtime.Scheme),
	encodeGV schema.GroupVersion,
	resource schema.GroupResource,
) (*runtime.Scheme, generic.RESTOptions, *etcd3testing.EtcdTestServer) {
	t.Helper()

	scheme := runtime.NewScheme()
	install(scheme)
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	codecs := serializer.NewCodecFactory(scheme)

	server, etcdStorage := etcd3testing.NewUnsecuredEtcd3TestClientServer(t)
	etcdStorage.Codec = codecs.LegacyCodec(encodeGV)

	restOptions := generic.RESTOptions{
		StorageConfig:           etcdStorage.ForResource(resource),
		Decorator:               generic.UndecoratedStorage,
		DeleteCollectionWorkers: 1,
		ResourcePrefix:          resource.Resource,
	}

	return scheme, restOptions, server
}
