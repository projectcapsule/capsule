// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func budgetScaleGVK(req admission.Request) schema.GroupVersionKind {
	// Scale's Kind is autoscaling/Scale; Resource identifies the parent.
	switch {
	case req.Resource.Group == "apps" && req.Resource.Resource == "deployments":
		return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	case req.Resource.Group == "apps" && req.Resource.Resource == "statefulsets":
		return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}
	case req.Resource.Group == "apps" && req.Resource.Resource == "replicasets":
		return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	case req.Resource.Group == "" && req.Resource.Resource == "replicationcontrollers":
		return schema.GroupVersionKind{Version: "v1", Kind: "ReplicationController"}
	default:
		return schema.GroupVersionKind{}
	}
}

func isBudgetScale(req admission.Request) bool {
	return req.SubResource == "scale" && req.Operation == admissionv1.Update && !budgetScaleGVK(req).Empty()
}

func budgetRequestGVK(req admission.Request) schema.GroupVersionKind {
	if isBudgetScale(req) {
		return budgetScaleGVK(req)
	}

	return requestGVK(req)
}

func matchesBudgetScale(req admission.Request) bool {
	if !isBudgetScale(req) {
		return false
	}

	type scale struct {
		Spec struct {
			Replicas int64 `json:"replicas"`
		} `json:"spec"`
	}

	var obj, old scale
	if json.Unmarshal(req.Object.Raw, &obj) != nil || json.Unmarshal(req.OldObject.Raw, &old) != nil {
		return true
	}

	return obj.Spec.Replicas != old.Spec.Replicas
}

func hasBudgetReplicaConstraints(bodies []*rules.NamespaceRuleEnforceBody, gvk schema.GroupVersionKind) bool {
	return slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		return hasEvictableBounds(budgetSettings(body)) && budgetTargets(body.Workloads, gvk)
	})
}

func budgetRequestWorkload(ctx context.Context, reader client.Reader, decoder admission.Decoder, req admission.Request, old, obj genericObject, bodies []*rules.NamespaceRuleEnforceBody) (budgetWorkload, bool, error) {
	gvk := budgetRequestGVK(req)
	if gvk.Group == "" && gvk.Kind == "Pod" {
		return budgetWorkload{gvk: gvk, name: obj.Name, labels: obj.Labels}, old == nil || !maps.Equal(old.Labels, obj.Labels), nil
	}

	full := &unstructured.Unstructured{}
	if err := decoder.Decode(req, full); err != nil {
		return budgetWorkload{}, false, err
	}

	if isBudgetScale(req) {
		return budgetScaledWorkload(ctx, reader, req, full)
	}

	value, err := budgetTemplateWorkload(full)
	if err != nil || old == nil {
		return value, true, err
	}

	previous := &unstructured.Unstructured{}
	if err := decoder.DecodeRaw(req.OldObject, previous); err != nil {
		return value, false, err
	}

	previousValue, err := budgetTemplateWorkload(previous)
	if err != nil {
		return value, false, err
	}

	changed := !maps.Equal(previousValue.labels, value.labels)
	if hasBudgetReplicaConstraints(bodies, gvk) && value.replicas != nil && previousValue.replicas != nil {
		changed = changed || *value.replicas != *previousValue.replicas
	}

	return value, changed, nil
}

func budgetScaledWorkload(ctx context.Context, reader client.Reader, req admission.Request, scale *unstructured.Unstructured) (budgetWorkload, bool, error) {
	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(budgetScaleGVK(req))

	if err := reader.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, parent); err != nil {
		return budgetWorkload{}, false, fmt.Errorf("read scale parent: %w", err)
	}

	if scale.GetUID() != "" && scale.GetUID() != parent.GetUID() {
		return budgetWorkload{}, false, fmt.Errorf("scale parent UID changed; retry")
	}

	value, err := budgetTemplateWorkload(parent)
	if err != nil {
		return value, false, err
	}

	replicas, _, err := unstructured.NestedInt64(scale.Object, "spec", "replicas")
	if err != nil {
		return value, false, err
	}

	value.replicas = &replicas

	return value, true, nil
}

func budgetTemplateWorkload(obj *unstructured.Unstructured) (budgetWorkload, error) {
	value := budgetWorkload{gvk: obj.GroupVersionKind(), name: obj.GetName()}

	var err error

	value.labels, err = budgetTemplateLabels(obj)
	if err != nil {
		return value, err
	}

	if budgetScalable(value.gvk) {
		replicas, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if err != nil {
			return value, err
		}

		if !found {
			replicas = 1
		}

		value.replicas = &replicas
	}

	return value, nil
}
