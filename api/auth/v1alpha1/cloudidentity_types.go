package v1alpha1

// CloudProvider defines the cloud provider.
// +enum
type CloudProvider string

const (
	// CloudProviderAWS is the cloud provider AWS.
	CloudProviderAWS CloudProvider = "aws"

	// CloudProviderAzure is the cloud provider Azure.
	CloudProviderAzure CloudProvider = "azure"

	// CloudProviderGCP is the cloud provider GCP.
	CloudProviderGCP CloudProvider = "gcp"
)

// CloudIdentitySpec defines the desired state of CloudIdentity
type CloudIdentitySpec struct {
	// Provider is the cloud provider.
	Provider CloudProvider `json:"provider"`
}
