package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WebhookStatus reflects the status of defined webhooks.
type WebhookStatus struct {
	// Name is the name of the webhook.
	Name string `json:"name"`

	// URL is the URL of the webhook.
	URL string `json:"url"`

	// LastReceived is the last time the webhook was received.
	LastReceived *metav1.Time `json:"lastReceived,omitempty"`

	// LastTriggered is the last time the webhook was triggered.
	LastTriggered *metav1.Time `json:"lastTriggered,omitempty"`

	// ReceivedCount is the number of times the webhook was received.
	ReceivedCount int64 `json:"receivedCount,omitempty"`

	// TriggersCount is the number of times the webhook was triggered.
	TriggersCount int64 `json:"triggersCount,omitempty"`

	// RejectedCount is the number of times the webhook was rejected.
	RejectedCount int64 `json:"rejectedCount,omitempty"`

	// FilteredCount is the number of times the webhook was filtered.
	FilteredCount int64 `json:"filteredCount,omitempty"`

	// LastRejection is the last time, reason and message the webhook was rejected.
	LastRejection *LastRejectionStatus `json:"lastRejection,omitempty"`
}

// LastRejectionStatus reflects the last time, reason and message the webhook was rejected.
type LastRejectionStatus struct {
	// RejectedAt is the time the webhook was rejected.
	RejectedAt *metav1.Time `json:"rejectedAt,omitempty"`

	// Reason is the reason the webhook was rejected.
	Reason string `json:"reason,omitempty"`

	// Message is the message of the webhook rejection.
	Message string `json:"message,omitempty"`
}
