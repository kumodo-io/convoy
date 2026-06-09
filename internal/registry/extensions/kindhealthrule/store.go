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

package kindhealthrule

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"

	"github.com/kumodo-io/convoy/api/extensions"
	"github.com/kumodo-io/convoy/internal/registry"
)

// NewREST returns RESTStorage for KindHealthRule. The resource has no status
// subresource, so only the main endpoint is returned.
func NewREST(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter) (*registry.REST, error) {
	strategy := NewStrategy(scheme)

	store := &genericregistry.Store{
		NewFunc:                   func() runtime.Object { return &extensions.KindHealthRule{} },
		NewListFunc:               func() runtime.Object { return &extensions.KindHealthRuleList{} },
		PredicateFunc:             MatchKindHealthRule,
		DefaultQualifiedResource:  extensions.Resource("kindhealthrules"),
		SingularQualifiedResource: extensions.Resource("kindhealthrule"),

		CreateStrategy: strategy,
		UpdateStrategy: strategy,
		DeleteStrategy: strategy,

		TableConvertor: newTableConvertor(),
	}

	options := &generic.StoreOptions{RESTOptions: optsGetter, AttrFunc: GetAttrs}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, err
	}

	return &registry.REST{Store: store}, nil
}
