// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

// BreakRequestSpec defines the desired state of BreakRequest.
type BreakRequestSpec struct {
	// TemplateName the name of the template to use for this request
	// +kubebuilder:validation:Required
	TemplateName string `json:"templateName"`
	// Params the parameters to use for the template.
	Params *runtime.RawExtension `json:"params,omitempty"`
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

// BreakRequestStatus defines the observed state of BreakRequest.
type BreakRequestStatus struct {
	// Review refers to the subject that either approved or denied the request
	Review *ReviewInfo `json:"review,omitempty"`
	// Template properties copied from the assigned template
	Template *TemplateProperties `json:"template,omitempty"`
	// The Approved properties are set when the request is approved.
	Approved *ApprovedProperties `json:"approved,omitempty"`
	// Shows timestamps between approval and termination of the request.
	Active *ActivePeriod `json:"active,omitempty"`
	// The time until which the BreakRequest should be retained after it expires (e.g. for auditing).
	// If zero, the BreakRequest can be deleted immediately after expiring.
	KeepUntil metav1.Time `json:"keepUntil,omitempty"`
	// Conditions applied to the request.
	// Known conditions are "Requested", "Pending", "Denied", "Approved", "Active" and "Expired".
	// The latest condition is reflected in the phase.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +kubebuilder:validation:Enum=Requested;Pending;Denied;Approved;Active;Expired
	Phase RequestPhase `json:"phase,omitempty"`
}

// ActivePeriod represents the time window when a request is active.
type ActivePeriod struct {
	ActiveFrom  metav1.Time `json:"from,omitempty"`
	ActiveUntil metav1.Time `json:"until,omitempty"`
}

// TemplateProperties contains properties copied from the assigned template.
type TemplateProperties struct {
	// The templates that are created by this request, provided by the template.
	Templates []runtime.RawExtension `json:"templates,omitempty"`
	// ParamSchema template parameter schema
	ParamSchema runtime.RawExtension `json:"paramSchema,omitempty"`
	// The default duration of the BreakRequest referencing this template should be valid for.
	DefaultDuration *metav1.Duration `json:"defaultDuration,omitempty"`
	// The max allowed duration of the BreakRequest referencing this template should be valid for.
	MaxDuration metav1.Duration `json:"maxDuration,omitempty"`
	// The duration of this BreakRequest will be kept in the system after it has been expired (eg. auditing purposes)
	// If not set, the BreakRequest will be deleted after expiring.
	KeepFor breaktheglass.ExtendedDuration `json:"keepFor,omitempty"`
}

// ApprovedProperties contains the properties set when a request is approved.
type ApprovedProperties struct {
	KeepFor   breaktheglass.ExtendedDuration `json:"keepFor,omitempty"`
	Duration  *metav1.Duration               `json:"duration,omitempty"`
	StartTime metav1.Time                    `json:"startTime,omitempty"`
	Templates []runtime.RawExtension         `json:"templates,omitempty"`
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

func init() {
	SchemeBuilder.Register(&BreakRequest{}, &BreakRequestList{})
}
