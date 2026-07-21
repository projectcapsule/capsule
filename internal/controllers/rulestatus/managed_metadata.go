// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func (r Manager) reconcileManagedMetadata(ctx context.Context, instance *capsulev1beta2.RuleStatus, previous, current []*rules.NamespaceRuleBodyNamespace) error {
	if r.RESTConfig == nil {
		return fmt.Errorf("REST config is required for managed metadata reconciliation")
	}

	dynamicClient, err := dynamic.NewForConfig(r.RESTConfig)
	if err != nil {
		return err
	}
	manager := ruleStatusFieldManager(instance)

	namespaceGVK := schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	previousLabels, previousAnnotations := managedMetadataForGVK(namespaceGVK, previous)
	labels, annotations := managedMetadataForGVK(namespaceGVK, current)
	if hasMetadata(previousLabels, previousAnnotations) || hasMetadata(labels, annotations) {
		if err := reconcileObjectManagedMetadata(ctx, dynamicClient, schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, namespaceGVK, "", instance.GetNamespace(), previousLabels, previousAnnotations, labels, annotations, manager); err != nil {
			return err
		}
	}

	if !hasManagedNamespacedMetadata(previous) && !hasManagedNamespacedMetadata(current) {
		return nil
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.RESTConfig)
	if err != nil {
		return err
	}
	resourceLists, discoveryErr := discoveryClient.ServerPreferredResources()
	if discoveryErr != nil && len(resourceLists) == 0 {
		return discoveryErr
	}
	for _, resourceList := range resourceLists {
		gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range resourceList.APIResources {
			if !resource.Namespaced || strings.Contains(resource.Name, "/") || !slices.Contains(resource.Verbs, "list") || !slices.Contains(resource.Verbs, "patch") {
				continue
			}
			gvr, gvk := gv.WithResource(resource.Name), gv.WithKind(resource.Kind)
			previousLabels, previousAnnotations := managedMetadataForGVK(gvk, previous)
			labels, annotations := managedMetadataForGVK(gvk, current)
			if !hasMetadata(previousLabels, previousAnnotations) && !hasMetadata(labels, annotations) {
				continue
			}
			items, err := dynamicClient.Resource(gvr).Namespace(instance.GetNamespace()).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			for i := range items.Items {
				if err := reconcileObjectManagedMetadata(ctx, dynamicClient, gvr, gvk, instance.GetNamespace(), items.Items[i].GetName(), previousLabels, previousAnnotations, labels, annotations, manager); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func reconcileObjectManagedMetadata(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, gvk schema.GroupVersionKind, namespace, name string, previousLabels, previousAnnotations, labels, annotations map[string]string, manager string) error {
	removedLabels := removedMetadataKeys(previousLabels, labels)
	removedAnnotations := removedMetadataKeys(previousAnnotations, annotations)
	if hasRemovedMetadata(removedLabels, removedAnnotations) {
		if err := removeManagedMetadata(ctx, dynamicClient, gvr, namespace, name, removedLabels, removedAnnotations); err != nil {
			return err
		}
	}
	return applyManagedMetadata(ctx, dynamicClient, gvr, gvk, namespace, name, labels, annotations, manager)
}

func removedMetadataKeys(previous, current map[string]string) map[string]any {
	removed := map[string]any{}
	for key := range previous {
		if _, ok := current[key]; !ok {
			removed[key] = nil
		}
	}
	return removed
}

func hasRemovedMetadata(labels, annotations map[string]any) bool {
	return len(labels) > 0 || len(annotations) > 0
}

func removeManagedMetadata(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string, labels, annotations map[string]any) error {
	metadata := map[string]any{}
	if len(labels) > 0 {
		metadata["labels"] = labels
	}
	if len(annotations) > 0 {
		metadata["annotations"] = annotations
	}
	raw, err := json.Marshal(map[string]any{"metadata": metadata})
	if err != nil {
		return err
	}
	var resource dynamic.ResourceInterface = dynamicClient.Resource(gvr)
	if namespace != "" {
		resource = dynamicClient.Resource(gvr).Namespace(namespace)
	}
	_, err = resource.Patch(ctx, name, types.MergePatchType, raw, metav1.PatchOptions{})
	return err
}

func hasMetadata(labels, annotations map[string]string) bool {
	return len(labels) > 0 || len(annotations) > 0
}

func hasManagedNamespacedMetadata(bodies []*rules.NamespaceRuleBodyNamespace) bool {
	for _, body := range bodies {
		if body == nil || body.Enforce == nil {
			continue
		}
		for _, rule := range body.Enforce.Metadata {
			if !metadataRuleHasManagedValues(rule) {
				continue
			}
			for _, kind := range rule.Kinds {
				if strings.TrimSpace(kind) != "Namespace" {
					return true
				}
			}
		}
	}
	return false
}

func metadataRuleHasManagedValues(rule rules.MetadataRule) bool {
	for _, policy := range rule.Labels {
		if policy.Managed != nil {
			return true
		}
	}
	for _, policy := range rule.Annotations {
		if policy.Managed != nil {
			return true
		}
	}
	return false
}

func managedMetadataForGVK(gvk schema.GroupVersionKind, bodies []*rules.NamespaceRuleBodyNamespace) (map[string]string, map[string]string) {
	labels, annotations := map[string]string{}, map[string]string{}
	for _, body := range bodies {
		if body == nil || body.Enforce == nil {
			continue
		}
		for _, rule := range body.Enforce.Metadata {
			if !rule.MatchesGroupVersionKind(gvk) {
				continue
			}
			for key, policy := range rule.Labels {
				if policy.Managed != nil {
					labels[key] = *policy.Managed
				}
			}
			for key, policy := range rule.Annotations {
				if policy.Managed != nil {
					annotations[key] = *policy.Managed
				}
			}
		}
	}
	return labels, annotations
}

func hasManagedMetadata(bodies []*rules.NamespaceRuleBodyNamespace) bool {
	for _, body := range bodies {
		if body == nil || body.Enforce == nil {
			continue
		}
		for _, rule := range body.Enforce.Metadata {
			for _, policy := range rule.Labels {
				if policy.Managed != nil {
					return true
				}
			}
			for _, policy := range rule.Annotations {
				if policy.Managed != nil {
					return true
				}
			}
		}
	}
	return false
}

func applyManagedMetadata(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, gvk schema.GroupVersionKind, namespace, name string, labels, annotations map[string]string, manager string) error {
	metadata := map[string]any{"name": name, "labels": labels, "annotations": annotations}
	var resource dynamic.ResourceInterface = dynamicClient.Resource(gvr)
	if namespace != "" {
		metadata["namespace"] = namespace
		resource = dynamicClient.Resource(gvr).Namespace(namespace)
	}
	payload := map[string]any{"apiVersion": gvk.GroupVersion().String(), "kind": gvk.Kind, "metadata": metadata}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	force := true
	_, err = resource.Patch(ctx, name, types.ApplyPatchType, raw, metav1.PatchOptions{FieldManager: manager, Force: &force})
	return err
}

func ruleStatusFieldManager(instance *capsulev1beta2.RuleStatus) string {
	sum := sha256.Sum256([]byte(instance.GetNamespace() + "/" + instance.GetName() + "/" + string(instance.GetUID())))
	return "projectcapsule.dev/rulestatus-" + hex.EncodeToString(sum[:8])
}
