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

package gitrepository_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	"k8s.io/apiserver/pkg/registry/rest"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	"github.com/kumodo-io/convoy/api/source"
	sourceinstall "github.com/kumodo-io/convoy/api/source/install"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
	gitrepositorystorage "github.com/kumodo-io/convoy/internal/registry/source/gitrepository"
)

const (
	dummyGitRepositoryName = "dummy-gitrepository"
	dummyGitRepositoryURL  = "https://dummy.example.com/service.git"
)

func newStorage(t *testing.T) (*genericregistry.Store, *gitrepositorystorage.StatusREST, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, sourceinstall.Install, sourcev1alpha1.SchemeGroupVersion, source.Resource("gitrepositories"))

	restStorage, statusStorage, err := gitrepositorystorage.NewREST(scheme, restOptions, 4)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, statusStorage, server
}

func validNewGitRepository(namespace string) *source.GitRepository {
	return &source.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyGitRepositoryName,
			Namespace: namespace,
		},
		Spec: source.GitRepositorySpec{
			URL: dummyGitRepositoryURL,
			Auth: source.GitRepositoryAuthSpec{
				Type: source.GitRepositoryAuthTypeNone,
			},
		},
	}
}

func invalidNewGitRepositroyNoURL(namespace string) *source.GitRepository {
	return &source.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyGitRepositoryName,
			Namespace: namespace,
		},
		Spec: source.GitRepositorySpec{
			Auth: source.GitRepositoryAuthSpec{
				Type: source.GitRepositoryAuthTypeNone,
			},
		},
	}
}

func invalidNewGitRepositroyNoAuth(namespace string) *source.GitRepository {
	return &source.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyGitRepositoryName,
			Namespace: namespace,
		},
		Spec: source.GitRepositorySpec{
			URL: dummyGitRepositoryURL,
		},
	}
}

func invalidNewGitRepositroyUnknownAuthType(namespace string) *source.GitRepository {
	return &source.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyGitRepositoryName,
			Namespace: namespace,
		},
		Spec: source.GitRepositorySpec{
			URL: dummyGitRepositoryURL,
			Auth: source.GitRepositoryAuthSpec{
				Type: "unknown",
			},
		},
	}
}

func invalidNewGitRepositoryMultipleAuthConfigs(namespace string) *source.GitRepository {
	return &source.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyGitRepositoryName,
			Namespace: namespace,
		},
		Spec: source.GitRepositorySpec{
			URL: dummyGitRepositoryURL,
			Auth: source.GitRepositoryAuthSpec{
				Type:   source.GitRepositoryAuthTypeToken,
				Token:  &authv1.TokenAuthSpec{},
				SSHKey: &authv1.SSHKeyAuthSpec{},
			},
		},
	}
}

func invalidNewGitRepositoryMismatchAuthConfig(namespace string) *source.GitRepository {
	return &source.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyGitRepositoryName,
			Namespace: namespace,
		},
		Spec: source.GitRepositorySpec{
			URL: dummyGitRepositoryURL,
			Auth: source.GitRepositoryAuthSpec{
				Type:  source.GitRepositoryAuthTypeSSHKey,
				Token: &authv1.TokenAuthSpec{},
			},
		},
	}
}

func TestCreate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	test.TestCreate(
		validNewGitRepository(ns),
		invalidNewGitRepositroyNoURL(ns),
		invalidNewGitRepositroyNoAuth(ns),
		invalidNewGitRepositroyUnknownAuthType(ns),
		invalidNewGitRepositoryMultipleAuthConfigs(ns),
		invalidNewGitRepositoryMismatchAuthConfig(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestUpdate(
		validNewGitRepository(test.TestNamespace()),
		// valid update: adjust the path filters
		func(obj runtime.Object) runtime.Object {
			r := obj.(*source.GitRepository)
			r.Spec.IncludePaths = []string{"deploy/"}
			return r
		},
		// invalid update: clears the required url
		func(obj runtime.Object) runtime.Object {
			r := obj.(*source.GitRepository)
			r.Spec.URL = ""
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewGitRepository(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewGitRepository(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewGitRepository(test.TestNamespace()))
}

func TestStatusUpdate(t *testing.T) {
	storage, statusStorage, server := newStorage(t)
	defer server.Terminate(t)

	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)

	if _, err := storage.Create(ctx, validNewGitRepository(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error creating gitrepository: %v", err)
	}

	obj, err := statusStorage.Get(ctx, dummyGitRepositoryName, &metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error getting gitrepository: %v", err)
	}
	repo := obj.(*source.GitRepository)

	repo.Status.ReleaseCount = 3
	// A spec change submitted through /status must be ignored.
	repo.Spec.URL = "https://changed.example.com"

	updatedObj, _, err := statusStorage.Update(
		ctx,
		repo.Name,
		rest.DefaultUpdatedObjectInfo(repo),
		rest.ValidateAllObjectFunc,
		rest.ValidateAllObjectUpdateFunc,
		false,
		&metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}

	updated := updatedObj.(*source.GitRepository)
	if updated.Status.ReleaseCount != 3 {
		t.Errorf("expected status.releaseCount to persist, got %d", updated.Status.ReleaseCount)
	}
	if updated.Spec.URL != dummyGitRepositoryURL {
		t.Errorf("expected spec change to be ignored by /status, got url=%q", updated.Spec.URL)
	}
}
