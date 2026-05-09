package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	GroupName  = "extensions.convoy.kumodo.io"
	APIVersion = "v1alpha1"
)

var schemeBuilder runtime.SchemeBuilder

var (
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: APIVersion}
	localSchemeBuilder = &schemeBuilder
	AddToScheme        = localSchemeBuilder.AddToScheme
)

func init() {
	localSchemeBuilder.Register(addKnownTypes)
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(
		SchemeGroupVersion,
		&KindHealthRule{},
		&KindHealthRuleList{},
		&ResourceHealthRule{},
		&ResourceHealthRuleList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)

	return nil
}

func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}
