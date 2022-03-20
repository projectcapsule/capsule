package v1alpha1

const (
	ForbiddenNodeLabelsAnnotation            = "capsule.clastix.io/forbidden-node-labels"
	ForbiddenNodeLabelsRegexpAnnotation      = "capsule.clastix.io/forbidden-node-labels-regexp"
	ForbiddenNodeAnnotationsAnnotation       = "capsule.clastix.io/forbidden-node-annotations"
	ForbiddenNodeAnnotationsRegexpAnnotation = "capsule.clastix.io/forbidden-node-annotations-regexp"
	CASecretNameAnnotation                   = "capsule.clastix.io/ca-secret-name"
	TLSSecretNameAnnotation                  = "capsule.clastix.io/tls-secret-name"
	MutatingWebhookConfigurationName         = "capsule.clastix.io/mutating-webhook-configuration-name"
	ValidatingWebhookConfigurationName       = "capsule.clastix.io/validating-webhook-configuration-name"
)
