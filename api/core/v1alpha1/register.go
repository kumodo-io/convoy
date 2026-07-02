package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

const (
	GroupName  = "core.convoy.kumodo.io"
	APIVersion = "v1alpha1"
)

var schemeBuilder runtime.SchemeBuilder

var (
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: APIVersion}
	localSchemeBuilder = &schemeBuilder
	AddToScheme        = localSchemeBuilder.AddToScheme
)

func init() {
	localSchemeBuilder.Register(addKnownTypes, authv1.AddToScheme)
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(
		SchemeGroupVersion,
		&Cluster{},
		&ClusterList{},
		&Release{},
		&ReleaseList{},
	)

	// Whitelist the fields a Release may be filtered on via --field-selector.
	// Without this the apiserver rejects any non-default field label.
	if err := scheme.AddFieldLabelConversionFunc(
		SchemeGroupVersion.WithKind("Release"),
		func(label, value string) (string, string, error) {
			switch label {
			case "metadata.name", "metadata.namespace",
				"spec.clusterRef.name", "spec.clusterRef.namespace":
				return label, value, nil
			default:
				return "", "", fmt.Errorf("field label %q not supported for Release", label)
			}
		},
	); err != nil {
		return err
	}

	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)

	return nil
}

func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}
