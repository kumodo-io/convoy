package v1alpha1

// RuleSpec defines a health assessment rule along with target resources.
type RuleSpec struct {
	// ApiVersion defines the target API group/version (ex. apps/v1) for resources
	// being assessed.
	ApiVersion string `json:"apiVersion"`

	// Resources defines the list of resources of given group/version.
	Resources []string `json:"resources"`

	// RuleExpression is the inlined CEL expression to evaluate.
	RuleExpression string `json:"ruleExpression"`

	// MessageExpression is the inlined CEL expression to evaluate for the health check message.
	MessageExpression string `json:"messageExpression,omitempty"`
}
