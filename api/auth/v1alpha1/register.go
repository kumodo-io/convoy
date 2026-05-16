package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	GroupName  = ""
	APIVersion = "v1alpha1"
)

var schemeBuilder runtime.SchemeBuilder

var (
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: APIVersion}
	localSchemeBuilder = &schemeBuilder
	AddToScheme        = localSchemeBuilder.AddToScheme
)
