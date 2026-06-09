package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HealthRule defines a health check rule.
type HealthRule struct {
	// Expression is the inlined CEL expression to evaluate.
	Expression string `json:"expression"`

	// ProgressDeadline is the maximum time to wait for the health check to complete.
	ProgressDeadline metav1.Duration `json:"progressDeadline"`

	// MessageExpression is the inlined CEL expression to evaluate for the health check message.
	MessageExpression string `json:"messageExpression,omitempty"`
}
