package extensions

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type HealthRule struct {
	Expression        string
	ProgressDeadline  metav1.Duration
	MessageExpression string
}

type KindHealthRuleSpec struct {
	HealthRule
	Target KindTarget
}

type KindTarget struct {
	Group   string
	Kind    string
	Version string
}

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

type ResourceHealthRuleSpec struct {
	HealthRule
	Target ResourceTarget
}

type ResourceTarget struct {
	Group     string
	Kind      string
	Version   string
	Name      string
	Namespace string
}

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
