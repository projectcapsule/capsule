// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

// JSONSchemaFormExtensionKey is the vendor-extension keyword understood by
// Capsule form consumers inside a JSON Schema property.
const JSONSchemaFormExtensionKey = "x-capsule-form"

// JSONSchemaFormWidget identifies a form widget supplied by Capsule.
type JSONSchemaFormWidget string

const (
	// JSONSchemaFormWidgetKubernetesResource renders a selector whose options
	// are loaded from the Kubernetes API using discovery and the requestor's
	// credentials.
	JSONSchemaFormWidgetKubernetesResource JSONSchemaFormWidget = "kubernetes-resource"

	// JSONSchemaFormRequestNamespace resolves to the namespace in which the
	// BreakRequest is being created.
	JSONSchemaFormRequestNamespace = "request"

	// JSONSchemaFormAllNamespaces lists a namespaced GVK across all namespaces.
	JSONSchemaFormAllNamespaces = "*"

	// JSONSchemaFormDefaultOptionTemplate is used for both the label and value
	// when their corresponding option template is omitted.
	JSONSchemaFormDefaultOptionTemplate = "{{ .metadata.name }}"
)

// JSONSchemaFormExtension describes optional form behavior for a JSON Schema
// property. It remains a JSON Schema vendor extension and therefore does not
// alter validation of the submitted parameter value.
//
// +kubebuilder:object:generate=true
type JSONSchemaFormExtension struct {
	// Widget selects the form control used for the property.
	// +kubebuilder:validation:Enum=kubernetes-resource
	Widget JSONSchemaFormWidget `json:"widget"`

	// Source describes the Kubernetes resources used as selectable options.
	Source *KubernetesResourceFormSource `json:"source"`

	// Option controls how each Kubernetes object is presented and converted to
	// the JSON Schema property's string value.
	// +optional
	Option *KubernetesResourceFormOption `json:"option,omitempty"`
}

// KubernetesResourceFormSource identifies an arbitrary Kubernetes GVK to list
// for a kubernetes-resource form widget.
//
// +kubebuilder:object:generate=true
type KubernetesResourceFormSource struct {
	// APIVersion of the resource, for example "v1" or "gateway.networking.k8s.io/v1".
	APIVersion string `json:"apiVersion"`

	// Kind of the resource, for example "Secret" or "GatewayClass".
	Kind string `json:"kind"`

	// Namespace controls the list scope for namespaced resources. "request"
	// selects the BreakRequest namespace, "*" selects all namespaces, and any
	// other value selects that literal namespace. When omitted, form consumers
	// use the BreakRequest namespace for namespaced GVKs and cluster scope for
	// cluster-scoped GVKs.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// LabelSelector is an optional Kubernetes label-selector expression applied
	// to the list request.
	// +optional
	LabelSelector string `json:"labelSelector,omitempty"`

	// FieldSelector is an optional Kubernetes field-selector expression applied
	// to the list request.
	// +optional
	FieldSelector string `json:"fieldSelector,omitempty"`
}

// KubernetesResourceFormOption controls how one listed Kubernetes object is
// displayed and serialized. Both fields use Go text/template syntax with the
// unstructured Kubernetes object as the root value.
//
// +kubebuilder:object:generate=true
type KubernetesResourceFormOption struct {
	// LabelTemplate renders the human-readable option label. It defaults to
	// "{{ .metadata.name }}".
	// +optional
	LabelTemplate string `json:"labelTemplate,omitempty"`

	// ValueTemplate renders the value stored in the BreakRequest parameters. It
	// defaults to "{{ .metadata.name }}".
	// +optional
	ValueTemplate string `json:"valueTemplate,omitempty"`
}
