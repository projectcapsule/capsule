// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func (r Manager) reconcileManagedMetadata(ctx context.Context, instance *capsulev1beta2.RuleStatus, bodies []*rules.NamespaceRuleBodyNamespace) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.RESTConfig)
	if err != nil {
		return err
	}
	dynamicClient, err := dynamic.NewForConfig(r.RESTConfig)
	if err != nil {
		return err
	}
	resourceLists, discoveryErr := discoveryClient.ServerPreferredResources()
	if discoveryErr != nil && len(resourceLists) == 0 {
		return discoveryErr
	}
	manager := ruleStatusFieldManager(instance)

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
			items, err := dynamicClient.Resource(gvr).Namespace(instance.GetNamespace()).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			labels, annotations := managedMetadataForGVK(gvk, bodies)
			for i := range items.Items {
				if err := applyManagedMetadata(ctx, dynamicClient, gvr, gvk, instance.GetNamespace(), items.Items[i].GetName(), labels, annotations, manager); err != nil {
					return err
				}
			}
		}
	}

	namespaceGVK := schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	labels, annotations := managedMetadataForGVK(namespaceGVK, bodies)
	return applyManagedMetadata(ctx, dynamicClient, schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, namespaceGVK, "", instance.GetNamespace(), labels, annotations, manager)
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
