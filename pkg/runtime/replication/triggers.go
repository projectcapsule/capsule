// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	admissionrequestv1 "k8s.io/api/admission/v1"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	admissionapi "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

// Target identifies a replication incarnation, so delayed work cannot affect a
// deleted and recreated object with the same name.
type Target struct {
	Name, Namespace string
	UID             types.UID
}

type Notifier interface {
	Notify(target Target, interval time.Duration) bool
}

const (
	TriggerPath            = "/replications/triggers/"
	TriggerAnnotation      = meta.ReplicationTriggerAnnotation
	TriggerIndexerField    = "spec.triggers.match"
	TriggerEnabledKey      = "enabled"
	DefaultTriggerInterval = 10 * time.Second
)

// MatchID identifies an immutable native admission predicate, independently of
// audience and replication identity. A changed predicate gets a different path,
// so requests from an obsolete webhook cannot activate an edited trigger.
func MatchID(trigger *capsulev1beta2.TenantResourceTrigger) string {
	projection := struct {
		Rule              admissionv1.RuleWithOperations `json:"rule"`
		NamespaceSelector *metav1.LabelSelector          `json:"namespaceSelector"`
		ObjectSelector    *metav1.LabelSelector          `json:"objectSelector"`
		Conditions        []admissionv1.MatchCondition   `json:"conditions"`
	}{trigger.RuleWithOperations, trigger.NamespaceSelector, trigger.ObjectSelector, trigger.MatchConditions}

	encoded, err := json.Marshal(projection)
	if err != nil {
		return ""
	}

	hash := sha256.Sum256(encoded)

	return hex.EncodeToString(hash[:])
}

func Interval(spec *capsulev1beta2.TenantResourceCommonSpec) time.Duration {
	if spec.TriggerMinInterval.Duration >= time.Second {
		return spec.TriggerMinInterval.Duration
	}

	return DefaultTriggerInterval
}

// Managed excludes both current and legacy Capsule management metadata. It is
// an event eligibility filter, never an authorization or ownership assertion.
func Managed(obj *metav1.PartialObjectMetadata) bool {
	if obj == nil {
		return false
	}

	// The legacy managed-by label holds a tenant name, not a management role.
	// In particular, controller/resources/replications are valid tenant names.
	for _, key := range []string{meta.NewManagedByCapsuleLabel, meta.CreatedByCapsuleLabel, meta.ProtectedByCapsuleLabel} {
		switch obj.Labels[key] {
		case meta.ValueController, meta.ValueControllerResources, meta.ValueControllerReplications:
			return true
		}
	}

	for _, key := range []string{meta.ResourcesLabel, meta.ReplicationProtectionLabel, meta.LimitRangeLabel, meta.NetworkPolicyLabel, meta.ResourceQuotaLabel, meta.RuleQuotaLabel, meta.RolebindingLabel} {
		if _, present := obj.Labels[key]; present {
			return true
		}
	}

	for _, owner := range obj.OwnerReferences {
		if strings.HasPrefix(owner.APIVersion, capsulev1beta2.GroupVersion.Group+"/") {
			return true
		}
	}

	return false
}

// BookkeepingRequest excludes controller updates to replication parents, including
// notification patches and status writes. User changes remain eligible even when
// the parent subscribes to its own kind. No persistent management marker is added.
func BookkeepingRequest(req *admissionrequestv1.AdmissionRequest) bool {
	return req.Operation == admissionrequestv1.Update && req.Resource.Group == capsulev1beta2.GroupVersion.Group &&
		(req.Resource.Resource == "tenantresources" || req.Resource.Resource == "globaltenantresources") &&
		users.IsControllerServiceAccount(req.UserInfo.Username)
}

// Webhook lets the API server evaluate native admission CEL (including
// authorizer) and selectors. The path carries the exact predicate identity.
func Webhook(trigger *capsulev1beta2.TenantResourceTrigger, namespace string, client admissionv1.WebhookClientConfig) admissionv1.ValidatingWebhook {
	id := MatchID(trigger)

	rule := *trigger.RuleWithOperations.DeepCopy()
	if slices.Contains(rule.Operations, admissionv1.OperationAll) {
		rule.Operations = []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete}
	}

	rule.Scope = ptr.To(admissionv1.NamespacedScope)

	conditions := append([]admissionv1.MatchCondition(nil), trigger.MatchConditions...)
	conditions = append(conditions, admissionv1.MatchCondition{Name: "capsule-trigger-dry-run", Expression: "!request.dryRun"})

	conditions = append(conditions, admissionv1.MatchCondition{Name: "capsule-trigger-unmanaged", Expression: unmanagedExpression("object") + " && " + unmanagedExpression("oldObject")})

	name, controllerNamespace := configuration.ControllerServiceAccount()
	if name != "" && controllerNamespace != "" {
		username := "system:serviceaccount:" + controllerNamespace + ":" + name
		conditions = append(conditions, admissionv1.MatchCondition{Name: "capsule-trigger-bookkeeping", Expression: "!(request.operation == 'UPDATE' && request.resource.group == " + admissionapi.CelQuote(capsulev1beta2.GroupVersion.Group) + " && request.resource.resource in ['tenantresources', 'globaltenantresources'] && request.userInfo.username == " + admissionapi.CelQuote(username) + ")"})
	}

	if namespace != "" {
		conditions = append(conditions, admissionv1.MatchCondition{Name: "capsule-trigger-namespace", Expression: "request.namespace == " + admissionapi.CelQuote(namespace)})
	}

	return admissionv1.ValidatingWebhook{
		Name:                    "trigger-" + id[:40] + ".projectcapsule.dev",
		ClientConfig:            admissionapi.DynamicClientWithPath(client, TriggerPath+id),
		Rules:                   []admissionv1.RuleWithOperations{rule},
		NamespaceSelector:       trigger.NamespaceSelector.DeepCopy(),
		ObjectSelector:          trigger.ObjectSelector.DeepCopy(),
		MatchConditions:         conditions,
		FailurePolicy:           ptr.To(admissionv1.Ignore),
		MatchPolicy:             ptr.To(admissionv1.Equivalent),
		SideEffects:             ptr.To(admissionv1.SideEffectClassNoneOnDryRun),
		TimeoutSeconds:          new(int32(2)),
		AdmissionReviewVersions: []string{"v1"},
	}
}

func unmanagedExpression(variable string) string {
	lifecycle := []string{meta.NewManagedByCapsuleLabel, meta.CreatedByCapsuleLabel, meta.ProtectedByCapsuleLabel}
	legacy := []string{meta.ResourcesLabel, meta.ReplicationProtectionLabel, meta.LimitRangeLabel, meta.NetworkPolicyLabel, meta.ResourceQuotaLabel, meta.RuleQuotaLabel, meta.RolebindingLabel}

	return fmt.Sprintf("(%[1]s == null || !has(%[1]s.metadata) || ((!has(%[1]s.metadata.labels) || !%[1]s.metadata.labels.exists(k, (k in %[2]s && %[1]s.metadata.labels[k] in ['controller','resources','replications']) || k in %[3]s)) && (!has(%[1]s.metadata.ownerReferences) || !%[1]s.metadata.ownerReferences.exists(o, o.apiVersion.startsWith('%[4]s/')))))", variable, admissionapi.CelStringList(lifecycle), admissionapi.CelStringList(legacy), capsulev1beta2.GroupVersion.Group)
}
