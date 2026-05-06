// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// +kubebuilder:object:generate=true
type WebhookOptions struct {
	// If enabled, the request is only sent to admission if the user is mentioned
	// As Part of the Capsule Users
	// +kubebuilder:default=false
	CapsuleUsers bool `json:"capsuleUsers"`

	// If enabled, the request is only sent to admission if the user is mentioned
	// As Part of the Capsule Administrators
	// +kubebuilder:default=false
	Administrators bool `json:"administrators"`
}

func NewValidatingWebhook(in *ValidatingWebhook, c admissionregistrationv1.WebhookClientConfig, users rbac.UserListSpec, admins rbac.UserListSpec) (admissionregistrationv1.ValidatingWebhook, error) {
	out := admissionregistrationv1.ValidatingWebhook{
		Name:                    in.Name,
		Rules:                   in.Rules,
		FailurePolicy:           in.FailurePolicy,
		MatchPolicy:             in.MatchPolicy,
		NamespaceSelector:       in.NamespaceSelector,
		ObjectSelector:          in.ObjectSelector,
		SideEffects:             in.SideEffects,
		TimeoutSeconds:          in.TimeoutSeconds,
		AdmissionReviewVersions: in.AdmissionReviewVersions,
	}

	// Path lives under ClientConfig in upstream
	out.ClientConfig = DynamicClientWithPath(c, *in.Path)

	if len(in.MatchConditions) > 0 {
		out.MatchConditions = append([]admissionregistrationv1.MatchCondition(nil), in.MatchConditions...)

		return out, nil
	}

	conds := BuildGatingUserCondition(in.Options, users, admins)
	if len(conds) > 0 {
		out.MatchConditions = conds
	}

	return out, nil
}

// +kubebuilder:object:generate=true
type ValidatingWebhook struct {
	// The name of the admission webhook.
	// Name should be fully qualified, e.g., imagepolicy.kubernetes.io, where
	// "imagepolicy" is the name of the webhook, and kubernetes.io is the name
	// of the organization.
	// Required.
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`

	// `path` is the URL path which will be sent in any request to
	// this service.
	Path *string `json:"path" protobuf:"bytes,3,opt,name=path"`

	// Capsule Custom Admission Options
	// +optional
	Options WebhookOptions `json:"opts"`

	// Rules describes what operations on what resources/subresources the webhook cares about.
	// The webhook cares about an operation if it matches _any_ Rule.
	// However, in order to prevent ValidatingAdmissionWebhooks and MutatingAdmissionWebhooks
	// from putting the cluster in a state which cannot be recovered from without completely
	// disabling the plugin, ValidatingAdmissionWebhooks and MutatingAdmissionWebhooks are never called
	// on admission requests for ValidatingWebhookConfiguration and MutatingWebhookConfiguration objects.
	// +listType=atomic
	Rules []admissionregistrationv1.RuleWithOperations `json:"rules,omitempty" protobuf:"bytes,3,rep,name=rules"`

	// FailurePolicy defines how unrecognized errors from the admission endpoint are handled -
	// allowed values are Ignore or Fail. Defaults to Fail.
	// +optional
	FailurePolicy *admissionregistrationv1.FailurePolicyType `json:"failurePolicy,omitempty" protobuf:"bytes,4,opt,name=failurePolicy,casttype=FailurePolicyType"`

	// matchPolicy defines how the "rules" list is used to match incoming requests.
	// Allowed values are "Exact" or "Equivalent".
	//
	// - Exact: match a request only if it exactly matches a specified rule.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// but "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would not be sent to the webhook.
	//
	// - Equivalent: match a request if modifies a resource listed in rules, even via another API group or version.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// and "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would be converted to apps/v1 and sent to the webhook.
	//
	// Defaults to "Equivalent"
	// +optional
	MatchPolicy *admissionregistrationv1.MatchPolicyType `json:"matchPolicy,omitempty" protobuf:"bytes,9,opt,name=matchPolicy,casttype=MatchPolicyType"`

	// NamespaceSelector decides whether to run the webhook on an object based
	// on whether the namespace for that object matches the selector. If the
	// object itself is a namespace, the matching is performed on
	// object.metadata.labels. If the object is another cluster scoped resource,
	// it never skips the webhook.
	//
	// For example, to run the webhook on any objects whose namespace is not
	// associated with "runlevel" of "0" or "1";  you will set the selector as
	// follows:
	// "namespaceSelector": {
	//   "matchExpressions": [
	//     {
	//       "key": "runlevel",
	//       "operator": "NotIn",
	//       "values": [
	//         "0",
	//         "1"
	//       ]
	//     }
	//   ]
	// }
	//
	// If instead you want to only run the webhook on any objects whose
	// namespace is associated with the "environment" of "prod" or "staging";
	// you will set the selector as follows:
	// "namespaceSelector": {
	//   "matchExpressions": [
	//     {
	//       "key": "environment",
	//       "operator": "In",
	//       "values": [
	//         "prod",
	//         "staging"
	//       ]
	//     }
	//   ]
	// }
	//
	// See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// for more examples of label selectors.
	//
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty" protobuf:"bytes,5,opt,name=namespaceSelector"`

	// ObjectSelector decides whether to run the webhook based on if the
	// object has matching labels. objectSelector is evaluated against both
	// the oldObject and newObject that would be sent to the webhook, and
	// is considered to match if either object matches the selector. A null
	// object (oldObject in the case of create, or newObject in the case of
	// delete) or an object that cannot have labels (like a
	// DeploymentRollback or a PodProxyOptions object) is not considered to
	// match.
	// Use the object selector only if the webhook is opt-in, because end
	// users may skip the admission webhook by setting the labels.
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	ObjectSelector *metav1.LabelSelector `json:"objectSelector,omitempty" protobuf:"bytes,10,opt,name=objectSelector"`

	// SideEffects states whether this webhook has side effects.
	// Acceptable values are: None, NoneOnDryRun (webhooks created via v1beta1 may also specify Some or Unknown).
	// Webhooks with side effects MUST implement a reconciliation system, since a request may be
	// rejected by a future step in the admission chain and the side effects therefore need to be undone.
	// Requests with the dryRun attribute will be auto-rejected if they match a webhook with
	// sideEffects == Unknown or Some.
	SideEffects *admissionregistrationv1.SideEffectClass `json:"sideEffects" protobuf:"bytes,6,opt,name=sideEffects,casttype=SideEffectClass"`

	// TimeoutSeconds specifies the timeout for this webhook. After the timeout passes,
	// the webhook call will be ignored or the API call will fail based on the
	// failure policy.
	// The timeout value must be between 1 and 30 seconds.
	// Default to 10 seconds.
	// +optional
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty" protobuf:"varint,7,opt,name=timeoutSeconds"`

	// AdmissionReviewVersions is an ordered list of preferred `AdmissionReview`
	// versions the Webhook expects. API server will try to use first version in
	// the list which it supports. If none of the versions specified in this list
	// supported by API server, validation will fail for this object.
	// If a persisted webhook configuration specifies allowed versions and does not
	// include any versions known to the API Server, calls to the webhook will fail
	// and be subject to the failure policy.
	// +listType=atomic
	AdmissionReviewVersions []string `json:"admissionReviewVersions" protobuf:"bytes,8,rep,name=admissionReviewVersions"`

	// MatchConditions is a list of conditions that must be met for a request to be sent to this
	// webhook. Match conditions filter requests that have already been matched by the rules,
	// namespaceSelector, and objectSelector. An empty list of matchConditions matches all requests.
	// There are a maximum of 64 match conditions allowed.
	//
	// The exact matching logic is (in order):
	//   1. If ANY matchCondition evaluates to FALSE, the webhook is skipped.
	//   2. If ALL matchConditions evaluate to TRUE, the webhook is called.
	//   3. If any matchCondition evaluates to an error (but none are FALSE):
	//      - If failurePolicy=Fail, reject the request
	//      - If failurePolicy=Ignore, the error is ignored and the webhook is skipped
	//
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	// +optional
	MatchConditions []admissionregistrationv1.MatchCondition `json:"matchConditions,omitempty" patchMergeKey:"name" patchStrategy:"merge" protobuf:"bytes,11,opt,name=matchConditions"`
}

func NewMutatingWebhook(in *MutatingWebhook, c admissionregistrationv1.WebhookClientConfig, users rbac.UserListSpec, admins rbac.UserListSpec) (admissionregistrationv1.MutatingWebhook, error) {
	out := admissionregistrationv1.MutatingWebhook{
		Name:                    in.Name,
		Rules:                   in.Rules,
		FailurePolicy:           in.FailurePolicy,
		ReinvocationPolicy:      in.ReinvocationPolicy,
		MatchPolicy:             in.MatchPolicy,
		NamespaceSelector:       in.NamespaceSelector,
		ObjectSelector:          in.ObjectSelector,
		SideEffects:             in.SideEffects,
		TimeoutSeconds:          in.TimeoutSeconds,
		AdmissionReviewVersions: in.AdmissionReviewVersions,
	}

	// Path lives under ClientConfig in upstream
	out.ClientConfig = DynamicClientWithPath(c, *in.Path)

	if len(in.MatchConditions) > 0 {
		out.MatchConditions = append([]admissionregistrationv1.MatchCondition(nil), in.MatchConditions...)

		return out, nil
	}

	conds := BuildGatingUserCondition(in.Options, users, admins)
	if len(conds) > 0 {
		out.MatchConditions = conds
	}

	return out, nil
}

// +kubebuilder:object:generate=true
type MutatingWebhook struct {
	// The name of the admission webhook.
	// Name should be fully qualified, e.g., imagepolicy.kubernetes.io, where
	// "imagepolicy" is the name of the webhook, and kubernetes.io is the name
	// of the organization.
	// Required.
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`

	// `path` is the URL path which will be sent in any request to
	// this service.
	Path *string `json:"path" protobuf:"bytes,3,opt,name=path"`

	// Capsule Custom Admission Options
	// +optional
	Options WebhookOptions `json:"opts,omitzero"`

	// Rules describes what operations on what resources/subresources the webhook cares about.
	// The webhook cares about an operation if it matches _any_ Rule.
	// However, in order to prevent ValidatingAdmissionWebhooks and MutatingAdmissionWebhooks
	// from putting the cluster in a state which cannot be recovered from without completely
	// disabling the plugin, ValidatingAdmissionWebhooks and MutatingAdmissionWebhooks are never called
	// on admission requests for ValidatingWebhookConfiguration and MutatingWebhookConfiguration objects.
	// +listType=atomic
	Rules []admissionregistrationv1.RuleWithOperations `json:"rules,omitempty" protobuf:"bytes,3,rep,name=rules"`

	// FailurePolicy defines how unrecognized errors from the admission endpoint are handled -
	// allowed values are Ignore or Fail. Defaults to Fail.
	// +optional
	FailurePolicy *admissionregistrationv1.FailurePolicyType `json:"failurePolicy,omitempty" protobuf:"bytes,4,opt,name=failurePolicy,casttype=FailurePolicyType"`

	// matchPolicy defines how the "rules" list is used to match incoming requests.
	// Allowed values are "Exact" or "Equivalent".
	//
	// - Exact: match a request only if it exactly matches a specified rule.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// but "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would not be sent to the webhook.
	//
	// - Equivalent: match a request if modifies a resource listed in rules, even via another API group or version.
	// For example, if deployments can be modified via apps/v1, apps/v1beta1, and extensions/v1beta1,
	// and "rules" only included `apiGroups:["apps"], apiVersions:["v1"], resources: ["deployments"]`,
	// a request to apps/v1beta1 or extensions/v1beta1 would be converted to apps/v1 and sent to the webhook.
	//
	// Defaults to "Equivalent"
	// +optional
	MatchPolicy *admissionregistrationv1.MatchPolicyType `json:"matchPolicy,omitempty" protobuf:"bytes,9,opt,name=matchPolicy,casttype=MatchPolicyType"`

	// NamespaceSelector decides whether to run the webhook on an object based
	// on whether the namespace for that object matches the selector. If the
	// object itself is a namespace, the matching is performed on
	// object.metadata.labels. If the object is another cluster scoped resource,
	// it never skips the webhook.
	//
	// For example, to run the webhook on any objects whose namespace is not
	// associated with "runlevel" of "0" or "1";  you will set the selector as
	// follows:
	// "namespaceSelector": {
	//   "matchExpressions": [
	//     {
	//       "key": "runlevel",
	//       "operator": "NotIn",
	//       "values": [
	//         "0",
	//         "1"
	//       ]
	//     }
	//   ]
	// }
	//
	// If instead you want to only run the webhook on any objects whose
	// namespace is associated with the "environment" of "prod" or "staging";
	// you will set the selector as follows:
	// "namespaceSelector": {
	//   "matchExpressions": [
	//     {
	//       "key": "environment",
	//       "operator": "In",
	//       "values": [
	//         "prod",
	//         "staging"
	//       ]
	//     }
	//   ]
	// }
	//
	// See
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
	// for more examples of label selectors.
	//
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty" protobuf:"bytes,5,opt,name=namespaceSelector"`

	// ObjectSelector decides whether to run the webhook based on if the
	// object has matching labels. objectSelector is evaluated against both
	// the oldObject and newObject that would be sent to the webhook, and
	// is considered to match if either object matches the selector. A null
	// object (oldObject in the case of create, or newObject in the case of
	// delete) or an object that cannot have labels (like a
	// DeploymentRollback or a PodProxyOptions object) is not considered to
	// match.
	// Use the object selector only if the webhook is opt-in, because end
	// users may skip the admission webhook by setting the labels.
	// Default to the empty LabelSelector, which matches everything.
	// +optional
	ObjectSelector *metav1.LabelSelector `json:"objectSelector,omitempty" protobuf:"bytes,11,opt,name=objectSelector"`

	// SideEffects states whether this webhook has side effects.
	// Acceptable values are: None, NoneOnDryRun (webhooks created via v1beta1 may also specify Some or Unknown).
	// Webhooks with side effects MUST implement a reconciliation system, since a request may be
	// rejected by a future step in the admission chain and the side effects therefore need to be undone.
	// Requests with the dryRun attribute will be auto-rejected if they match a webhook with
	// sideEffects == Unknown or Some.
	SideEffects *admissionregistrationv1.SideEffectClass `json:"sideEffects" protobuf:"bytes,6,opt,name=sideEffects,casttype=SideEffectClass"`

	// TimeoutSeconds specifies the timeout for this webhook. After the timeout passes,
	// the webhook call will be ignored or the API call will fail based on the
	// failure policy.
	// The timeout value must be between 1 and 30 seconds.
	// Default to 10 seconds.
	// +optional
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty" protobuf:"varint,7,opt,name=timeoutSeconds"`

	// AdmissionReviewVersions is an ordered list of preferred `AdmissionReview`
	// versions the Webhook expects. API server will try to use first version in
	// the list which it supports. If none of the versions specified in this list
	// supported by API server, validation will fail for this object.
	// If a persisted webhook configuration specifies allowed versions and does not
	// include any versions known to the API Server, calls to the webhook will fail
	// and be subject to the failure policy.
	// +listType=atomic
	AdmissionReviewVersions []string `json:"admissionReviewVersions" protobuf:"bytes,8,rep,name=admissionReviewVersions"`

	// reinvocationPolicy indicates whether this webhook should be called multiple times as part of a single admission evaluation.
	// Allowed values are "Never" and "IfNeeded".
	//
	// Never: the webhook will not be called more than once in a single admission evaluation.
	//
	// IfNeeded: the webhook will be called at least one additional time as part of the admission evaluation
	// if the object being admitted is modified by other admission plugins after the initial webhook call.
	// Webhooks that specify this option *must* be idempotent, able to process objects they previously admitted.
	// Note:
	// * the number of additional invocations is not guaranteed to be exactly one.
	// * if additional invocations result in further modifications to the object, webhooks are not guaranteed to be invoked again.
	// * webhooks that use this option may be reordered to minimize the number of additional invocations.
	// * to validate an object after all mutations are guaranteed complete, use a validating admission webhook instead.
	//
	// Defaults to "Never".
	// +optional
	ReinvocationPolicy *admissionregistrationv1.ReinvocationPolicyType `json:"reinvocationPolicy,omitempty" protobuf:"bytes,10,opt,name=reinvocationPolicy,casttype=ReinvocationPolicyType"`

	// MatchConditions is a list of conditions that must be met for a request to be sent to this
	// webhook. Match conditions filter requests that have already been matched by the rules,
	// namespaceSelector, and objectSelector. An empty list of matchConditions matches all requests.
	// There are a maximum of 64 match conditions allowed.
	//
	// The exact matching logic is (in order):
	//   1. If ANY matchCondition evaluates to FALSE, the webhook is skipped.
	//   2. If ALL matchConditions evaluate to TRUE, the webhook is called.
	//   3. If any matchCondition evaluates to an error (but none are FALSE):
	//      - If failurePolicy=Fail, reject the request
	//      - If failurePolicy=Ignore, the error is ignored and the webhook is skipped
	//
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	// +optional
	MatchConditions []admissionregistrationv1.MatchCondition `json:"matchConditions,omitempty" patchMergeKey:"name" patchStrategy:"merge" protobuf:"bytes,12,opt,name=matchConditions"`
}
