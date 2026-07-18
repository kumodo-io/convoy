package extensions

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type RuleSpec struct {
	ApiVersion        string
	Resources         []string
	RuleExpression    string
	MessageExpression string
}

type GlobalHealthRuleSetSpec struct {
	Rules []RuleSpec
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type GlobalHealthRuleSet struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec GlobalHealthRuleSetSpec
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type GlobalHealthRuleSetList struct {
	metav1.TypeMeta
	metav1.ListMeta
	Items []GlobalHealthRuleSet
}

type HealthRuleSetSpec struct {
	Rules []RuleSpec
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HealthRuleSet struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec HealthRuleSetSpec
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HealthRuleSetList struct {
	metav1.TypeMeta
	metav1.ListMeta
	Items []HealthRuleSet
}
