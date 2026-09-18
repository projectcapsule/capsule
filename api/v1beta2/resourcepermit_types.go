// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

// ResourcePermitSpec defines the desired state of ResourcePermit.
type ResourcePermitSpec struct {
	// Template references the template to use for this request.
	// +kubebuilder:validation:Required
	Template ResourcePermitTemplateReference `json:"template"`
	// Params the parameters to use for the template.
	Params *k8sruntime.RawExtension `json:"params,omitempty"`
	// Requesting actor for the resource permit.
	Requestor resourcepermit.AccessEntity `json:"requestor,omitempty"`
	// A reason on why the request is needed
	Reason string `json:"reason,omitempty"`
	// The duration of this ResourcePermit should be valid for.
	// If no duration was defined, the lifecycle is bound to the request itself -
	// if the request is deleted, it's the end of the duration.
	// The Request can also be Terminated by another automation via calling the ExpirePermit() API-Function.
	Duration *metav1.Duration `json:"duration,omitempty"`
	// Optional point in time when the permit should become active. Must be in the future.
	// If omitted, this is set to the current time. The Request must already be approved before the start time.
	// +optional
	// +kubebuilder:validation:Format=date-time
	// +kubebuilder:validation:Type=string
	StartTime *metav1.Time `json:"startTime,omitempty"`
}

const (
	ResourcePermitTemplateKind       = "ResourcePermitTemplate"
	GlobalResourcePermitTemplateKind = "GlobalResourcePermitTemplate"
)

// ResourcePermitTemplateReference identifies the namespaced or global template
// used by a ResourcePermit.
type ResourcePermitTemplateReference struct {
	// Kind of template being referenced.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ResourcePermitTemplate;GlobalResourcePermitTemplate
	Kind string `json:"kind"`
	// Name of the template being referenced.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
}

// GlobalResourcePermitTemplateReference is retained as a compatibility alias.
type GlobalResourcePermitTemplateReference = ResourcePermitTemplateReference

// ResolvedResourcePermitTemplateReference identifies the exact template version
// used to render a ResourcePermit.
type ResolvedResourcePermitTemplateReference struct {
	ResourcePermitTemplateReference `json:",inline"`

	// ResourceVersion of the template used to render the request resources.
	// +kubebuilder:validation:Required
	ResourceVersion string `json:"resourceVersion"`
}

// ResolvedGlobalResourcePermitTemplateReference is retained as a compatibility alias.
type ResolvedGlobalResourcePermitTemplateReference = ResolvedResourcePermitTemplateReference

// ResourcePermitStatus defines the observed state of ResourcePermit.
type ResourcePermitStatus struct {
	meta.ManagedResourcesStatus `json:",inline"`

	// Request contains the resolved template, execution identity, lifecycle
	// properties, and rendered resources presented for review.
	// +optional
	Request *ResourcePermitStatusRequest `json:"request,omitempty"`

	// Review refers to the subject that either approved or denied the request
	Review *ReviewInfo `json:"review,omitempty"`
	// Failure describes a recoverable preflight or activation failure. RetryPhase
	// is the trusted lifecycle phase Capsule resumes after a successful retry.
	// +optional
	Failure *ResourcePermitFailure `json:"failure,omitempty"`
	// Shows timestamps between approval and termination of the request.
	Active *ActivePeriod `json:"active,omitempty"`
	// The time until which the ResourcePermit should be retained after it expires (e.g. for auditing).
	// If unset, the ResourcePermit can be deleted immediately after expiring.
	KeepUntil *metav1.Time `json:"keepUntil,omitempty"`
	// Transitions is the chronological, append-only audit trail of lifecycle
	// changes. Conditions remain reserved for operational state such as Ready.
	// +optional
	Transitions []ResourcePermitTransition `json:"transitions,omitempty"`
	// Conditions describes current operational state such as readiness.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +kubebuilder:validation:Enum=Created;Requested;Pending;Denied;Approved;Active;Failed;Retrying;Expired
	Phase ResourcePermitPhase `json:"phase,omitempty"`
}

// ResourcePermitTransition records one authenticated lifecycle transition.
type ResourcePermitTransition struct {
	// Type identifies the lifecycle state entered by this transition. The
	// previous state can be derived from the preceding chronological entry.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Created;Requested;Pending;Denied;Approved;Active;Failed;Retrying;Expired
	Type ResourcePermitPhase `json:"type"`
	// Timestamp is when the transition was requested or performed.
	// +kubebuilder:validation:Required
	Timestamp metav1.Time `json:"timestamp"`
	// Actor is the authenticated user, ServiceAccount, or Capsule system actor
	// responsible for the transition. Group claims are deliberately not copied
	// into the audit trail.
	// +kubebuilder:validation:Required
	Actor ResourcePermitTransitionActor `json:"actor"`
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

// ResourcePermitTransitionActor is the compact identity recorded for a lifecycle
// transition. Authorization group claims remain available on the request and
// are not duplicated in each audit entry.
type ResourcePermitTransitionActor struct {
	// Name is the authenticated actor name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Type identifies the kind of authenticated actor.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=User;Group;System;ServiceAccount
	Type resourcepermit.AccessEntityType `json:"type"`
}

// ResourcePermitFailure records how a failed request can be retried while the
// Ready condition carries the detailed reason and message.
type ResourcePermitFailure struct {
	// Stage identifies whether the failure happened before review or while
	// activating an already approved request.
	// +kubebuilder:validation:Enum=Preflight;Activation
	Stage ResourcePermitFailureStage `json:"stage"`
	// RetryPhase is the phase Capsule resumes after recovery succeeds.
	// +kubebuilder:validation:Enum=Requested;Approved
	RetryPhase ResourcePermitPhase `json:"retryPhase"`
	// Reason is the stable machine-readable Ready condition reason.
	Reason string `json:"reason"`
	// Message contains the latest actionable failure returned by Kubernetes.
	Message string `json:"message"`
}

type ResourcePermitFailureStage string

const (
	ResourcePermitFailureStagePreflight  ResourcePermitFailureStage = "Preflight"
	ResourcePermitFailureStageActivation ResourcePermitFailureStage = "Activation"
)

// ActivePeriod represents the time window when a request is active.
type ActivePeriod struct {
	ActiveFrom  *metav1.Time `json:"from,omitempty"`
	ActiveUntil *metav1.Time `json:"until,omitempty"`
}

// ResourcePermitStatusRequest is the controller-resolved request presented for
// review and used as the source of truth for application and pruning.
type ResourcePermitStatusRequest struct {
	// Template identifies the exact template version used to render the request.
	// +optional
	Template *ResolvedResourcePermitTemplateReference `json:"template,omitempty"`

	// Impersonation is the resolved identity used for template context loading
	// and managed-resource actions. Capsule records its controller ServiceAccount
	// when no impersonation is configured.
	// +optional
	Impersonation *meta.NamespacedRFC1123ObjectReferenceWithNamespace `json:"impersonation,omitempty"`

	// Approvals is the approval policy copied from the resolved template. It is
	// immutable after rendering so an in-flight request is reviewed against the
	// policy presented with its resource snapshot.
	// +optional
	Approvals *resourcepermit.ApprovalSpec `json:"approvals,omitempty"`

	KeepFor   *resourcepermit.ExtendedDuration `json:"keepFor,omitempty"`
	Duration  *metav1.Duration                 `json:"duration,omitempty"`
	StartTime *metav1.Time                     `json:"startTime,omitempty"`
	// Resources contains the fully rendered manifests prepared for this request.
	// These resources are the source of truth for server-side apply and pruning;
	// source templates and rendering context are never copied into the request.
	Resources []apiruntime.RenderedResource `json:"resources,omitempty"`
}

// ReviewInfo contains information about the review of a request.
type ReviewInfo struct {
	// The Entity reviewing this request
	Reviewer *resourcepermit.AccessEntity `json:"reviewer,omitempty"`
	// The verdict made by the reviewing entity
	// +kubebuilder:validation:Enum=Pending;Denied;Approved
	Verdict ResourcePermitVerdict `json:"verdict,omitempty"`
	// Message with the review
	Message string `json:"message,omitempty"`
}

type ResourcePermitVerdict string

const (
	ResourcePermitVerdictDenied   ResourcePermitVerdict = "Denied"
	ResourcePermitVerdictApproved ResourcePermitVerdict = "Approved"
	ResourcePermitVerdictPending  ResourcePermitVerdict = "Pending"
)

type ResourcePermitPhase string

const (
	ResourcePermitPhaseCreated   ResourcePermitPhase = "Created"
	ResourcePermitPhaseRequested ResourcePermitPhase = "Requested"
	ResourcePermitPhasePending   ResourcePermitPhase = "Pending"
	ResourcePermitPhaseDenied    ResourcePermitPhase = "Denied"
	ResourcePermitPhaseApproved  ResourcePermitPhase = "Approved"
	ResourcePermitPhaseActive    ResourcePermitPhase = "Active"
	ResourcePermitPhaseFailed    ResourcePermitPhase = "Failed"
	ResourcePermitPhaseRetrying  ResourcePermitPhase = "Retrying"
	ResourcePermitPhaseExpired   ResourcePermitPhase = "Expired"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=rp
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

// ResourcePermit is the Schema for the ResourcePermits API.
type ResourcePermit struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourcePermitSpec   `json:"spec,omitempty"`
	Status ResourcePermitStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourcePermitList contains a list of ResourcePermit.
type ResourcePermitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ResourcePermit `json:"items"`
}
