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

	// Request contains the resolved template, execution identity, lifecycle
	// properties, and rendered resources presented for review.
	// +optional
	Request *BreakRequestStatusRequest `json:"request,omitempty"`

	// Review refers to the subject that either approved or denied the request
	Review *ReviewInfo `json:"review,omitempty"`
	// Failure describes a recoverable preflight or activation failure. RetryPhase
	// is the trusted lifecycle phase Capsule resumes after a successful retry.
	// +optional
	Failure *BreakRequestFailure `json:"failure,omitempty"`
	// Shows timestamps between approval and termination of the request.
	Active *ActivePeriod `json:"active,omitempty"`
	// The time until which the BreakRequest should be retained after it expires (e.g. for auditing).
	// If unset, the BreakRequest can be deleted immediately after expiring.
	KeepUntil *metav1.Time `json:"keepUntil,omitempty"`
	// Transitions is the chronological, append-only audit trail of lifecycle
	// changes. Conditions remain reserved for operational state such as Ready.
	// +optional
	Transitions []BreakRequestTransition `json:"transitions,omitempty"`
	// Conditions describes current operational state such as readiness.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +kubebuilder:validation:Enum=Created;Requested;Pending;Denied;Approved;Active;Failed;Retrying;Expired
	Phase RequestPhase `json:"phase,omitempty"`
}

// BreakRequestTransition records one authenticated lifecycle transition.
type BreakRequestTransition struct {
	// Type identifies the lifecycle state entered by this transition. The
	// previous state can be derived from the preceding chronological entry.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Created;Requested;Pending;Denied;Approved;Active;Failed;Retrying;Expired
	Type RequestPhase `json:"type"`
	// Timestamp is when the transition was requested or performed.
	// +kubebuilder:validation:Required
	Timestamp metav1.Time `json:"timestamp"`
	// Actor is the authenticated user, ServiceAccount, or Capsule system actor
	// responsible for the transition. Group claims are deliberately not copied
	// into the audit trail.
	// +kubebuilder:validation:Required
	Actor BreakRequestTransitionActor `json:"actor"`
	// Reason is a stable machine-readable explanation of the transition.
	// +kubebuilder:validation:Required
	Reason string `json:"reason"`
	// Message is the human-readable explanation of the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// EventTime is set after Capsule emits the Kubernetes lifecycle event for
	// this transition.
	// +optional
	EventTime *metav1.Time `json:"eventTime,omitempty"`
}

// BreakRequestTransitionActor is the compact identity recorded for a lifecycle
// transition. Authorization group claims remain available on the request and
// are not duplicated in each audit entry.
type BreakRequestTransitionActor struct {
	// Name is the authenticated actor name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Type identifies the kind of authenticated actor.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=User;Group;System;ServiceAccount
	Type breaktheglass.AccessEntityType `json:"type"`
}

// BreakRequestFailure records how a failed request can be retried while the
// Ready condition carries the detailed reason and message.
type BreakRequestFailure struct {
	// Stage identifies whether the failure happened before review or while
	// activating an already approved request.
	// +kubebuilder:validation:Enum=Preflight;Activation
	Stage RequestFailureStage `json:"stage"`
	// RetryPhase is the phase Capsule resumes after recovery succeeds.
	// +kubebuilder:validation:Enum=Requested;Approved
	RetryPhase RequestPhase `json:"retryPhase"`
	// Reason is the stable machine-readable Ready condition reason.
	Reason string `json:"reason"`
	// Message contains the latest actionable failure returned by Kubernetes.
	Message string `json:"message"`
}

type RequestFailureStage string

const (
	RequestFailureStagePreflight  RequestFailureStage = "Preflight"
	RequestFailureStageActivation RequestFailureStage = "Activation"
)

// ActivePeriod represents the time window when a request is active.
type ActivePeriod struct {
	ActiveFrom  *metav1.Time `json:"from,omitempty"`
	ActiveUntil *metav1.Time `json:"until,omitempty"`
}

// BreakRequestStatusRequest is the controller-resolved request presented for
// review and used as the source of truth for application and pruning.
type BreakRequestStatusRequest struct {
	// Template identifies the exact template version used to render the request.
	// +optional
	Template *ResolvedBreakRequestTemplateReference `json:"template,omitempty"`

	// Impersonation is the resolved identity used for template context loading
	// and managed-resource actions. Capsule records its controller ServiceAccount
	// when no impersonation is configured.
	// +optional
	Impersonation *meta.NamespacedRFC1123ObjectReferenceWithNamespace `json:"impersonation,omitempty"`

	// Approvals is the approval policy copied from the resolved template. It is
	// immutable after rendering so an in-flight request is reviewed against the
	// policy presented with its resource snapshot.
	// +optional
	Approvals *breaktheglass.ApprovalSpec `json:"approvals,omitempty"`

	KeepFor   *breaktheglass.ExtendedDuration `json:"keepFor,omitempty"`
	Duration  *metav1.Duration                `json:"duration,omitempty"`
	StartTime *metav1.Time                    `json:"startTime,omitempty"`
	// Resources contains the fully rendered manifests prepared for this request.
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
	RequestPhaseCreated   RequestPhase = "Created"
	RequestPhaseRequested RequestPhase = "Requested"
	RequestPhasePending   RequestPhase = "Pending"
	RequestPhaseDenied    RequestPhase = "Denied"
	RequestPhaseApproved  RequestPhase = "Approved"
	RequestPhaseActive    RequestPhase = "Active"
	RequestPhaseFailed    RequestPhase = "Failed"
	RequestPhaseRetrying  RequestPhase = "Retrying"
	RequestPhaseExpired   RequestPhase = "Expired"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.spec.reason`
// +kubebuilder:printcolumn:name="Verdict",type=string,JSONPath=`.status.review.verdict`
// +kubebuilder:printcolumn:name="ActiveFrom",type=string,JSONPath=`.status.active.from`,priority=10
// +kubebuilder:printcolumn:name="ActiveUntil",type=string,JSONPath=`.status.active.until`,priority=10
// +kubebuilder:printcolumn:name="Duration",type=string,JSONPath=`.status.request.duration`,priority=10
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`,priority=10
// +kubebuilder:printcolumn:name="Items",type="integer",JSONPath=".status.size",description="The number of managed resources"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"

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
