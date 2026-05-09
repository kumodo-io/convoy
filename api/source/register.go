package source

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const GroupName = "source.convoy.kumodo.io"

var (
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: runtime.APIVersionInternal}
	SchemeBuilder      = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme        = SchemeBuilder.AddToScheme
)

func Kind(kind string) schema.GroupKind {
	return SchemeGroupVersion.WithKind(kind).GroupKind()
}

func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(
		SchemeGroupVersion,
		&GitRepository{},
		&GitRepositoryList{},
		&HelmRegistry{},
		&HelmRegistryList{},
		&OCIRegistry{},
		&OCIRegistryList{},
	)
	return nil
}
