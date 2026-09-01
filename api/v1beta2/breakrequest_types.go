// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

// BreakRequestSpec defines the desired state of BreakRequest.
type BreakRequestSpec struct {
	// Template references the template to use for this request.
	// +kubebuilder:validation:Required
	Template BreakRequestTemplateReference `json:"template"`
	// Params the parameters to use for the template.
	Params *k8sruntime.RawExtension `json:"params,omitempty"`
	// Requesting actor for the access request.
	Requestor breaktheglass.AccessEntity `json:"requestor,omitempty"`
	// A reason on why the request is needed
	Reason string `json:"reason,omitempty"`
	// The duration of this BreakRequest should be valid for.
	// If no duration was defined, the lifecycle is bound to the request itself -
	// if the request is deleted, it's the end of the duration.
	// The Request can also be Terminated by another automation via calling the ExpireRequest() API-Function.
	Duration *metav1.Duration `json:"duration,omitempty"`
	// Optional point in time when the access should begin. Must be in the future.
	// If omitted, this is set to the current time. The Request must already be approved before the start time.
	// +optional
	// +kubebuilder:validation:Format=date-time
	// +kubebuilder:validation:Type=string
	StartTime *metav1.Time `json:"startTime,omitempty"`
}

const (
	BreakRequestTemplateKind       = "BreakRequestTemplate"
	GlobalBreakRequestTemplateKind = "GlobalBreakRequestTemplate"
)

// BreakRequestTemplateReference identifies the namespaced or global template
// used by a BreakRequest.
type BreakRequestTemplateReference struct {
	// Kind of template being referenced.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=BreakRequestTemplate;GlobalBreakRequestTemplate
	Kind string `json:"kind"`
	// Name of the template being referenced.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
}

// GlobalBreakRequestTemplateReference is retained as a compatibility alias.
type GlobalBreakRequestTemplateReference = BreakRequestTemplateReference

// ResolvedBreakRequestTemplateReference identifies the exact template version
// used to render a BreakRequest.
type ResolvedBreakRequestTemplateReference struct {
	BreakRequestTemplateReference `json:",inline"`

	// ResourceVersion of the template used to render the request resources.
	// +kubebuilder:validation:Required
	ResourceVersion string `json:"resourceVersion"`
}

// ResolvedGlobalBreakRequestTemplateReference is retained as a compatibility alias.
type ResolvedGlobalBreakRequestTemplateReference = ResolvedBreakRequestTemplateReference

// BreakRequestStatus defines the observed state of BreakRequest.
type BreakRequestStatus struct {
	meta.ManagedResourcesStatus `json:",inline"`

	// Template identifies the exact template version used to render the approved
	// resource snapshot.
	// +optional
	Template *ResolvedBreakRequestTemplateReference `json:"template,omitempty"`

	// ServiceAccount is the resolved identity used for template context loading
	// and managed-resource actions. Capsule records its controller ServiceAccount
	// when no impersonation is configured.
	// +optional
	ServiceAccount *meta.NamespacedRFC1123ObjectReferenceWithNamespace `json:"serviceAccount,omitempty"`

	// Review refers to the subject that either approved or denied the request
	Review *ReviewInfo `json:"review,omitempty"`
	// Approved contains the rendered resources and effective lifecycle properties
	// presented for approval. Once approved, this snapshot is authoritative.
	Approved *ApprovedProperties `json:"approved,omitempty"`
	// Shows timestamps between approval and termination of the request.
	Active *ActivePeriod `json:"active,omitempty"`
	// The time until which the BreakRequest should be retained after it expires (e.g. for auditing).
	// If unset, the BreakRequest can be deleted immediately after expiring.
	KeepUntil *metav1.Time `json:"keepUntil,omitempty"`
	// Conditions applied to the request.
	// Known conditions are "Ready", "Requested", "Pending", "Denied", "Approved", "Active" and "Expired".
	// The latest condition is reflected in the phase.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +kubebuilder:validation:Enum=Requested;Pending;Denied;Approved;Active;Expired
	Phase RequestPhase `json:"phase,omitempty"`
}

// ActivePeriod represents the time window when a request is active.
type ActivePeriod struct {
	ActiveFrom  *metav1.Time `json:"from,omitempty"`
	ActiveUntil *metav1.Time `json:"until,omitempty"`
}

// ApprovedProperties contains the properties set when a request is approved.
type ApprovedProperties struct {
	KeepFor   *breaktheglass.ExtendedDuration `json:"keepFor,omitempty"`
	Duration  *metav1.Duration                `json:"duration,omitempty"`
	StartTime *metav1.Time                    `json:"startTime,omitempty"`
	// Resources contains the fully rendered manifests approved for this request.
	// These resources are the source of truth for server-side apply and pruning;
	// source templates and rendering context are never copied into the request.
	Resources []apiruntime.RenderedResource `json:"resources,omitempty"`
}

// ReviewInfo contains information about the review of a request.
type ReviewInfo struct {
	// The Entity reviewing this request
	Reviewer *breaktheglass.AccessEntity `json:"reviewer,omitempty"`
	// The verdict made by the reviewing entity
	// +kubebuilder:validation:Enum=Pending;Denied;Approved
	Verdict RequestVerdict `json:"verdict,omitempty"`
	// Message with the review
	Message string `json:"message,omitempty"`
}

type RequestVerdict string

const (
	RequestVerdictDenied   RequestVerdict = "Denied"
	RequestVerdictApproved RequestVerdict = "Approved"
	RequestVerdictPending  RequestVerdict = "Pending"
)

type RequestPhase string

const (
	RequestPhaseRequested RequestPhase = "Requested"
	RequestPhasePending   RequestPhase = "Pending"
	RequestPhaseDenied    RequestPhase = "Denied"
	RequestPhaseApproved  RequestPhase = "Approved"
	RequestPhaseActive    RequestPhase = "Active"
	RequestPhaseExpired   RequestPhase = "Expired"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.spec.reason`
// +kubebuilder:printcolumn:name="Verdict",type=string,JSONPath=`.status.review.verdict`
// +kubebuilder:printcolumn:name="ActiveFrom",type=string,JSONPath=`.status.active.from`,priority=10
// +kubebuilder:printcolumn:name="ActiveUntil",type=string,JSONPath=`.status.active.until`,priority=10
// +kubebuilder:printcolumn:name="Duration",type=string,JSONPath=`.status.approved.duration`,priority=10
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`,priority=10
// +kubebuilder:printcolumn:name="Items",type="integer",JSONPath=".status.size",description="The number of managed resources"

// BreakRequest is the Schema for the BreakRequests API.
type BreakRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BreakRequestSpec   `json:"spec,omitempty"`
	Status BreakRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BreakRequestList contains a list of BreakRequest.
type BreakRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []BreakRequest `json:"items"`
}
