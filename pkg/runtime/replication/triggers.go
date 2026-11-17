// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"strings"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type Target struct {
	Name, Namespace string
	UID             types.UID
}

type Notifier interface {
	Notify(target Target, interval time.Duration) bool
}

const (
	TriggerAnnotation      = meta.ReplicationTriggerAnnotation
	TriggerIndexerField    = "spec.triggers.resource"
	TriggerEnabledKey      = "enabled"
	DefaultTriggerInterval = 10 * time.Second
	MaxTriggerResources    = 16
)

// ResourceKey selects subscriptions to one explicit resource and operation.
func ResourceKey(resource schema.GroupVersionResource, operation admissionv1.OperationType) string {
	return resource.Group + "/" + resource.Version + "/" + resource.Resource + "/" + string(operation)
}

// TriggerResources returns the bounded explicit watch set. Invalid stored input
// must not expand into an unbounded informer/index working set.
func TriggerResources(trigger *capsulev1beta2.TenantResourceTrigger) []schema.GroupVersionResource {
	if validateTriggerRules(trigger, "trigger") != nil {
		return nil
	}

	out := make([]schema.GroupVersionResource, 0, len(trigger.APIGroups)*len(trigger.APIVersions)*len(trigger.Resources))

	for _, group := range trigger.APIGroups {
		for _, version := range trigger.APIVersions {
			for _, resource := range trigger.Resources {
				out = append(out, schema.GroupVersionResource{Group: group, Version: version, Resource: resource})
			}
		}
	}

	return out
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
