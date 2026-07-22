// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

const managedMetadataListPageSize int64 = 500

type managedMetadataTarget struct {
	gvr schema.GroupVersionResource
	gvk schema.GroupVersionKind
}

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

	targets, err := managedMetadataTargets(r.RESTMapper, previous, current)
	if err != nil {
		return err
	}

	for _, target := range targets {
		previousLabels, previousAnnotations := managedMetadataForGVK(target.gvk, previous)
		labels, annotations := managedMetadataForGVK(target.gvk, current)

		if err := reconcileManagedMetadataTarget(
			ctx,
			dynamicClient,
			target,
			instance.GetNamespace(),
			previousLabels,
			previousAnnotations,
			labels,
			annotations,
			manager,
		); err != nil {
			return err
		}
	}

	return nil
}

func reconcileManagedMetadataTarget(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	target managedMetadataTarget,
	namespace string,
	previousLabels, previousAnnotations, labels, annotations map[string]string,
	manager string,
) error {
	continueToken := ""

	for {
		items, err := dynamicClient.Resource(target.gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
			Limit:    managedMetadataListPageSize,
			Continue: continueToken,
		})
		if err != nil {
			if isManagedMetadataObjectGone(err) {
				return nil
			}

			return fmt.Errorf("list %s in namespace %q: %w", target.gvr.String(), namespace, err)
		}

		for i := range items.Items {
			if err := reconcileObjectManagedMetadata(
				ctx,
				dynamicClient,
				target.gvr,
				target.gvk,
				namespace,
				items.Items[i].GetName(),
				previousLabels,
				previousAnnotations,
				labels,
				annotations,
				manager,
			); err != nil {
				return err
			}
		}

		continueToken = items.GetContinue()
		if continueToken == "" {
			return nil
		}
	}
}

func managedMetadataTargets(
	mapper k8smeta.RESTMapper,
	ruleSets ...[]*rules.NamespaceRuleBodyNamespace,
) ([]managedMetadataTarget, error) {
	targets := make(map[schema.GroupVersionResource]managedMetadataTarget)

	for _, bodies := range ruleSets {
		for _, body := range bodies {
			if body == nil || body.Enforce == nil {
				continue
			}

			for _, rule := range body.Enforce.Metadata {
				if !metadataRuleHasManagedValues(rule) {
					continue
				}

				if rule.HasWildcard() {
					return nil, fmt.Errorf("managed metadata requires concrete apiGroups and kinds")
				}

				for _, kind := range rule.Kinds {
					kind = strings.TrimSpace(kind)
					for _, apiGroup := range rule.StatusAPIGroups() {
						mapping, err := managedMetadataRESTMapping(mapper, apiGroup, kind)
						if err != nil {
							return nil, fmt.Errorf("resolve managed metadata target %q/%q: %w", apiGroup, kind, err)
						}

						if mapping.GroupVersionKind.Group == "" &&
							mapping.GroupVersionKind.Version == apiruntime.CoreAPIVersion &&
							mapping.GroupVersionKind.Kind == "Namespace" {
							continue
						}

						if mapping.Scope.Name() != k8smeta.RESTScopeNameNamespace {
							return nil, fmt.Errorf("managed metadata target %s is not namespaced", mapping.GroupVersionKind.String())
						}

						targets[mapping.Resource] = managedMetadataTarget{
							gvr: mapping.Resource,
							gvk: mapping.GroupVersionKind,
						}
					}
				}
			}
		}
	}

	out := make([]managedMetadataTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, target)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].gvr.String() < out[j].gvr.String()
	})

	return out, nil
}

func managedMetadataRESTMapping(
	mapper k8smeta.RESTMapper,
	apiGroup string,
	kind string,
) (*k8smeta.RESTMapping, error) {
	if mapper == nil {
		return nil, fmt.Errorf("REST mapper is required for managed metadata reconciliation")
	}

	apiGroup = strings.TrimSpace(apiGroup)
	if apiGroup == "" || apiGroup == apiruntime.CoreAPIVersion {
		return mapper.RESTMapping(schema.GroupKind{Kind: kind}, apiruntime.CoreAPIVersion)
	}

	if gv, err := schema.ParseGroupVersion(apiGroup); err == nil && strings.Contains(apiGroup, "/") {
		return mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: kind}, gv.Version)
	}

	return mapper.RESTMapping(schema.GroupKind{Group: apiGroup, Kind: kind})
}

func reconcileObjectManagedMetadata(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, gvk schema.GroupVersionKind, namespace, name string, previousLabels, previousAnnotations, labels, annotations map[string]string, manager string) error {
	removedLabels := removedMetadataKeys(previousLabels, labels)

	removedAnnotations := removedMetadataKeys(previousAnnotations, annotations)

	if hasRemovedMetadata(removedLabels, removedAnnotations) {
		if err := removeManagedMetadata(ctx, dynamicClient, gvr, namespace, name, removedLabels, removedAnnotations); err != nil {
			if isManagedMetadataObjectGone(err) {
				return nil
			}

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
	if isManagedMetadataObjectGone(err) {
		return nil
	}

	return err
}

func isManagedMetadataObjectGone(err error) bool {
	return apierrors.IsNotFound(err)
}

func ruleStatusFieldManager(instance *capsulev1beta2.RuleStatus) string {
	sum := sha256.Sum256([]byte(instance.GetNamespace() + "/" + instance.GetName() + "/" + string(instance.GetUID())))

	return "projectcapsule.dev/rulestatus-" + hex.EncodeToString(sum[:8])
}
