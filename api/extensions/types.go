package extensions

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type KindHealthRuleSpec struct{}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type KindHealthRule struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec KindHealthRuleSpec
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type KindHealthRuleList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []KindHealthRule
}

type ResourceHealthRuleSpec struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ResourceHealthRule struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec ResourceHealthRuleSpec
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ResourceHealthRuleList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []ResourceHealthRule
}
