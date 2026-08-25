// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package evaluator contains the admission-side resource calculations used by
// GlobalResourceQuota. The calculations are adapted from the Kubernetes
// ResourceQuota core evaluators at the version matching this module's
// Kubernetes dependencies.
package evaluator

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	resourcehelper "k8s.io/component-helpers/resource"
	storagehelpers "k8s.io/component-helpers/storage/volume"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const storageClassSuffix = ".storageclass.storage.k8s.io/"

var validationResources = sets.New(
	corev1.ResourceCPU,
	corev1.ResourceMemory,
	corev1.ResourceRequestsCPU,
	corev1.ResourceRequestsMemory,
	corev1.ResourceLimitsCPU,
	corev1.ResourceLimitsMemory,
)

var legacyObjectCountAliases = map[string]corev1.ResourceName{
	"configmaps":             corev1.ResourceConfigMaps,
	"resourcequotas":         corev1.ResourceQuotas,
	"replicationcontrollers": corev1.ResourceReplicationControllers,
	"secrets":                corev1.ResourceSecrets,
}

// Result holds one decoded admission request and its native quota usage.
type Result struct {
	NewUsage corev1.ResourceList
	OldUsage corev1.ResourceList
	New      runtime.Object
	Old      runtime.Object
}

// Evaluate decodes an admission request once and calculates the same native
// resource names used by the Kubernetes core quota evaluators.
func Evaluate(req admission.Request) (Result, bool, error) {
	if req.SubResource != "" && req.SubResource != "resize" && req.SubResource != "status" {
		return Result{}, false, nil
	}

	resourceName := req.Resource.Resource

	switch {
	case req.Resource.Group == "" && resourceName == "pods":
		return evaluatePod(req)
	case req.Resource.Group == "" && resourceName == "services":
		return evaluateService(req)
	case req.Resource.Group == "" && resourceName == "persistentvolumeclaims":
		return evaluatePVC(req)
	default:
		if req.SubResource != "" || req.Operation != "CREATE" {
			return Result{}, false, nil
		}

		object := &metav1.PartialObjectMetadata{}
		if err := json.Unmarshal(req.Object.Raw, object); err != nil {
			return Result{}, false, err
		}

		usage := objectCountUsage(req.Resource.Group, resourceName)

		return Result{NewUsage: usage, New: object}, true, nil
	}
}

func evaluatePod(req admission.Request) (Result, bool, error) {
	if req.Operation != "CREATE" && req.Operation != "UPDATE" {
		return Result{}, false, nil
	}

	newPod := &corev1.Pod{}
	if err := json.Unmarshal(req.Object.Raw, newPod); err != nil {
		return Result{}, false, fmt.Errorf("decode Pod: %w", err)
	}

	newUsage := podUsage(newPod, time.Now())
	result := Result{NewUsage: newUsage, New: newPod}

	if req.Operation == "UPDATE" {
		oldPod := &corev1.Pod{}
		if err := json.Unmarshal(req.OldObject.Raw, oldPod); err != nil {
			return Result{}, false, fmt.Errorf("decode old Pod: %w", err)
		}

		result.Old = oldPod
		result.OldUsage = podUsage(oldPod, time.Now())
	}

	return result, true, nil
}

func evaluateService(req admission.Request) (Result, bool, error) {
	if req.SubResource != "" || (req.Operation != "CREATE" && req.Operation != "UPDATE") {
		return Result{}, false, nil
	}

	service := &corev1.Service{}
	if err := json.Unmarshal(req.Object.Raw, service); err != nil {
		return Result{}, false, fmt.Errorf("decode Service: %w", err)
	}

	result := Result{NewUsage: serviceUsage(service), New: service}

	if req.Operation == "UPDATE" {
		oldService := &corev1.Service{}
		if err := json.Unmarshal(req.OldObject.Raw, oldService); err != nil {
			return Result{}, false, fmt.Errorf("decode old Service: %w", err)
		}

		result.Old = oldService
		result.OldUsage = serviceUsage(oldService)
	}

	return result, true, nil
}

func evaluatePVC(req admission.Request) (Result, bool, error) {
	if (req.SubResource != "" && req.SubResource != "status") ||
		(req.Operation != "CREATE" && req.Operation != "UPDATE") {
		return Result{}, false, nil
	}

	pvc := &corev1.PersistentVolumeClaim{}
	if err := json.Unmarshal(req.Object.Raw, pvc); err != nil {
		return Result{}, false, fmt.Errorf("decode PersistentVolumeClaim: %w", err)
	}

	result := Result{NewUsage: pvcUsage(pvc), New: pvc}

	if req.Operation == "UPDATE" {
		oldPVC := &corev1.PersistentVolumeClaim{}
		if err := json.Unmarshal(req.OldObject.Raw, oldPVC); err != nil {
			return Result{}, false, fmt.Errorf("decode old PersistentVolumeClaim: %w", err)
		}

		result.Old = oldPVC
		result.OldUsage = pvcUsage(oldPVC)
	}

	return result, true, nil
}

func objectCountUsage(group, resourceName string) corev1.ResourceList {
	countName := generic.ObjectCountQuotaResourceNameFor(
		schema.GroupResource{Group: group, Resource: resourceName},
	)
	one := *resource.NewQuantity(1, resource.DecimalSI)
	result := corev1.ResourceList{countName: one}

	if group == "" {
		if alias, ok := legacyObjectCountAliases[resourceName]; ok {
			result[alias] = one
		}
	}

	return result
}

func podUsage(pod *corev1.Pod, now time.Time) corev1.ResourceList {
	result := objectCountUsage("", "pods")
	if !quotaPod(pod, now) {
		return result
	}

	opts := resourcehelper.PodResourcesOptions{
		UseStatusResources:    true,
		SkipPodLevelResources: false,
	}
	requests := resourcehelper.PodRequests(pod, opts)
	limits := resourcehelper.PodLimits(pod, opts)
	addResourceList(result, podComputeUsage(requests, limits))

	return result
}

func podComputeUsage(requests, limits corev1.ResourceList) corev1.ResourceList {
	result := corev1.ResourceList{
		corev1.ResourcePods: *resource.NewQuantity(1, resource.DecimalSI),
	}

	addRequest := func(name, plain, prefixed corev1.ResourceName) {
		if quantity, found := requests[name]; found {
			result[plain] = quantity
			result[prefixed] = quantity
		}
	}

	addRequest(corev1.ResourceCPU, corev1.ResourceCPU, corev1.ResourceRequestsCPU)
	addRequest(corev1.ResourceMemory, corev1.ResourceMemory, corev1.ResourceRequestsMemory)
	addRequest(corev1.ResourceEphemeralStorage, corev1.ResourceEphemeralStorage, corev1.ResourceRequestsEphemeralStorage)

	if quantity, found := limits[corev1.ResourceCPU]; found {
		result[corev1.ResourceLimitsCPU] = quantity
	}

	if quantity, found := limits[corev1.ResourceMemory]; found {
		result[corev1.ResourceLimitsMemory] = quantity
	}

	if quantity, found := limits[corev1.ResourceEphemeralStorage]; found {
		result[corev1.ResourceLimitsEphemeralStorage] = quantity
	}

	for name, quantity := range requests {
		switch {
		case strings.HasPrefix(string(name), corev1.ResourceHugePagesPrefix):
			result[name] = quantity
			result[corev1.ResourceName(corev1.DefaultResourceRequestsPrefix+string(name))] = quantity
		case isExtendedResourceName(name):
			result[corev1.ResourceName(corev1.DefaultResourceRequestsPrefix+string(name))] = quantity
		}
	}

	return result
}

func serviceUsage(service *corev1.Service) corev1.ResourceList {
	result := objectCountUsage("", "services")
	result[corev1.ResourceServices] = *resource.NewQuantity(1, resource.DecimalSI)
	result[corev1.ResourceServicesLoadBalancers] = *resource.NewQuantity(0, resource.DecimalSI)
	result[corev1.ResourceServicesNodePorts] = *resource.NewQuantity(0, resource.DecimalSI)

	ports := int64(len(service.Spec.Ports))

	switch service.Spec.Type {
	case corev1.ServiceTypeClusterIP, corev1.ServiceTypeExternalName:
	case corev1.ServiceTypeNodePort:
		result[corev1.ResourceServicesNodePorts] = *resource.NewQuantity(ports, resource.DecimalSI)
	case corev1.ServiceTypeLoadBalancer:
		if ptr.Deref(service.Spec.AllocateLoadBalancerNodePorts, true) {
			result[corev1.ResourceServicesNodePorts] = *resource.NewQuantity(ports, resource.DecimalSI)
		} else {
			var count int64

			for _, port := range service.Spec.Ports {
				if port.NodePort != 0 {
					count++
				}
			}

			result[corev1.ResourceServicesNodePorts] = *resource.NewQuantity(count, resource.DecimalSI)
		}

		result[corev1.ResourceServicesLoadBalancers] = *resource.NewQuantity(1, resource.DecimalSI)
	}

	return result
}

func pvcUsage(pvc *corev1.PersistentVolumeClaim) corev1.ResourceList {
	result := objectCountUsage("", "persistentvolumeclaims")
	one := *resource.NewQuantity(1, resource.DecimalSI)
	result[corev1.ResourcePersistentVolumeClaims] = one

	storageClass := storagehelpers.GetPersistentVolumeClaimClass(pvc)
	if storageClass != "" {
		result[resourceByStorageClass(storageClass, corev1.ResourcePersistentVolumeClaims)] = one
	}

	requested, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		return result
	}

	rounded := requested.DeepCopy()

	_ = rounded.RoundUp(0)

	if allocated, ok := pvc.Status.AllocatedResources[corev1.ResourceStorage]; ok && allocated.Cmp(rounded) > 0 {
		rounded = allocated.DeepCopy()
		_ = rounded.RoundUp(0)
	}

	result[corev1.ResourceRequestsStorage] = rounded
	if storageClass != "" {
		result[resourceByStorageClass(storageClass, corev1.ResourceRequestsStorage)] = rounded
	}

	return result
}

func resourceByStorageClass(storageClass string, name corev1.ResourceName) corev1.ResourceName {
	return corev1.ResourceName(storageClass + storageClassSuffix + string(name))
}

// MatchesScopes mirrors generic quota scope matching.
func MatchesScopes(spec corev1.ResourceQuotaSpec, object runtime.Object) (bool, error) {
	requirements := make([]corev1.ScopedResourceSelectorRequirement, 0, len(spec.Scopes))
	for _, scope := range spec.Scopes {
		requirements = append(requirements, corev1.ScopedResourceSelectorRequirement{
			ScopeName: scope,
			Operator:  corev1.ScopeSelectorOpExists,
		})
	}

	if spec.ScopeSelector != nil {
		requirements = append(requirements, spec.ScopeSelector.MatchExpressions...)
	}

	for _, requirement := range requirements {
		matches, err := matchesScope(requirement, object)
		if err != nil || !matches {
			return matches, err
		}
	}

	return true, nil
}

func matchesScope(requirement corev1.ScopedResourceSelectorRequirement, object runtime.Object) (bool, error) {
	switch typed := object.(type) {
	case *corev1.Pod:
		return podMatchesScope(requirement, typed)
	case *corev1.PersistentVolumeClaim:
		return pvcMatchesScope(requirement, typed)
	default:
		return false, nil
	}
}

func podMatchesScope(requirement corev1.ScopedResourceSelectorRequirement, pod *corev1.Pod) (bool, error) {
	switch requirement.ScopeName {
	case corev1.ResourceQuotaScopeTerminating:
		return isTerminating(pod), nil
	case corev1.ResourceQuotaScopeNotTerminating:
		return !isTerminating(pod), nil
	case corev1.ResourceQuotaScopeBestEffort:
		return podQOS(pod) == corev1.PodQOSBestEffort, nil
	case corev1.ResourceQuotaScopeNotBestEffort:
		return podQOS(pod) != corev1.PodQOSBestEffort, nil
	case corev1.ResourceQuotaScopePriorityClass:
		if requirement.Operator == corev1.ScopeSelectorOpExists {
			return pod.Spec.PriorityClassName != "", nil
		}

		return requirementMatches(requirement, []string{pod.Spec.PriorityClassName})
	case corev1.ResourceQuotaScopeCrossNamespacePodAffinity:
		return usesCrossNamespacePodAffinity(pod), nil
	case corev1.ResourceQuotaScopeVolumeAttributesClass:
		return false, nil
	default:
		return false, nil
	}
}

func pvcMatchesScope(
	requirement corev1.ScopedResourceSelectorRequirement,
	pvc *corev1.PersistentVolumeClaim,
) (bool, error) {
	if requirement.ScopeName != corev1.ResourceQuotaScopeVolumeAttributesClass {
		return false, nil
	}

	values := sets.New[string]()
	if value := ptr.Deref(pvc.Spec.VolumeAttributesClassName, ""); value != "" {
		values.Insert(value)
	}

	if value := ptr.Deref(pvc.Status.CurrentVolumeAttributesClassName, ""); value != "" {
		values.Insert(value)
	}

	if pvc.Status.ModifyVolumeStatus != nil && pvc.Status.ModifyVolumeStatus.TargetVolumeAttributesClassName != "" {
		values.Insert(pvc.Status.ModifyVolumeStatus.TargetVolumeAttributesClassName)
	}

	if requirement.Operator == corev1.ScopeSelectorOpExists {
		return values.Len() > 0, nil
	}

	return requirementMatches(requirement, values.UnsortedList())
}

func requirementMatches(
	requirement corev1.ScopedResourceSelectorRequirement,
	values []string,
) (bool, error) {
	operator, err := scopeSelectorOperator(requirement.Operator)
	if err != nil {
		return false, err
	}

	labelRequirement, err := labels.NewRequirement(
		string(requirement.ScopeName),
		operator,
		requirement.Values,
	)
	if err != nil {
		return false, err
	}

	if len(values) == 0 {
		return labelRequirement.Matches(labels.Set{}), nil
	}

	for _, value := range values {
		if labelRequirement.Matches(labels.Set{string(requirement.ScopeName): value}) {
			return true, nil
		}
	}

	return false, nil
}

func scopeSelectorOperator(operator corev1.ScopeSelectorOperator) (selection.Operator, error) {
	switch operator {
	case corev1.ScopeSelectorOpIn:
		return selection.In, nil
	case corev1.ScopeSelectorOpNotIn:
		return selection.NotIn, nil
	case corev1.ScopeSelectorOpExists:
		return selection.Exists, nil
	case corev1.ScopeSelectorOpDoesNotExist:
		return selection.DoesNotExist, nil
	default:
		return "", fmt.Errorf("unsupported scope selector operator %q", operator)
	}
}

// ValidateConstraints preserves Kubernetes' historical requirement that every
// container explicitly sets CPU/memory resources when those resources are
// quota-controlled.
func ValidateConstraints(hard corev1.ResourceList, object runtime.Object) error {
	pod, ok := object.(*corev1.Pod)
	if !ok {
		return nil
	}

	// Kubernetes skips the legacy per-container presence check when a supported
	// Pod-level request or limit is set. Disabled Pod-level fields are removed by
	// the API server before validating admission webhooks receive the Pod.
	if resourcehelper.IsPodLevelResourcesSet(pod) {
		return nil
	}

	required := sets.New[corev1.ResourceName]()

	for name := range hard {
		if validationResources.Has(name) {
			required.Insert(name)
		}
	}

	missing := map[corev1.ResourceName][]string{}

	containers := append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...)

	for _, container := range containers {
		usage := podComputeUsage(container.Resources.Requests, container.Resources.Limits)
		for name := range required {
			if _, ok := usage[name]; !ok {
				missing[name] = append(missing[name], container.Name)
			}
		}
	}

	if len(missing) == 0 {
		return nil
	}

	parts := make([]string, 0, len(missing))
	for _, name := range sets.List(sets.KeySet(missing)) {
		parts = append(parts, fmt.Sprintf("%s for: %s", name, strings.Join(missing[name], ",")))
	}

	return fmt.Errorf("must specify %s", strings.Join(parts, "; "))
}

func quotaPod(pod *corev1.Pod, now time.Time) bool {
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		return false
	}

	if pod.DeletionTimestamp != nil && pod.DeletionGracePeriodSeconds != nil {
		deadline := pod.DeletionTimestamp.Add(time.Duration(*pod.DeletionGracePeriodSeconds) * time.Second)
		if now.After(deadline) {
			return false
		}
	}

	return true
}

func isTerminating(pod *corev1.Pod) bool {
	return pod.Spec.ActiveDeadlineSeconds != nil && *pod.Spec.ActiveDeadlineSeconds >= 0
}

func podQOS(pod *corev1.Pod) corev1.PodQOSClass {
	if pod.Status.QOSClass != "" {
		return pod.Status.QOSClass
	}

	requests := corev1.ResourceList{}
	limits := corev1.ResourceList{}
	guaranteed := true

	process := func(target corev1.ResourceList, resources corev1.ResourceList) {
		for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			if quantity, ok := resources[name]; ok && quantity.Sign() > 0 {
				addQuantity(target, name, quantity)
			}
		}
	}
	hasQoSLimits := func(resources corev1.ResourceList) bool {
		for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			quantity, ok := resources[name]
			if !ok || quantity.Sign() <= 0 {
				return false
			}
		}

		return true
	}

	if pod.Spec.Resources != nil {
		process(requests, pod.Spec.Resources.Requests)
		process(limits, pod.Spec.Resources.Limits)
		guaranteed = hasQoSLimits(pod.Spec.Resources.Limits)
	} else {
		containers := append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...)
		for _, container := range containers {
			process(requests, container.Resources.Requests)
			process(limits, container.Resources.Limits)

			if !hasQoSLimits(container.Resources.Limits) {
				guaranteed = false
			}
		}
	}

	if len(requests) == 0 && len(limits) == 0 {
		return corev1.PodQOSBestEffort
	}

	if guaranteed && len(requests) == len(limits) {
		for name, request := range requests {
			if limit, ok := limits[name]; !ok || request.Cmp(limit) != 0 {
				guaranteed = false

				break
			}
		}
	}

	if guaranteed && len(requests) == len(limits) {
		return corev1.PodQOSGuaranteed
	}

	return corev1.PodQOSBurstable
}

func addQuantity(target corev1.ResourceList, name corev1.ResourceName, quantity resource.Quantity) {
	current := target[name]
	current.Add(quantity)
	target[name] = current
}

func addResourceList(target, addition corev1.ResourceList) {
	for name, quantity := range addition {
		addQuantity(target, name, quantity)
	}
}

func isExtendedResourceName(name corev1.ResourceName) bool {
	value := string(name)

	return strings.Contains(value, "/") && !strings.Contains(value, corev1.ResourceDefaultNamespacePrefix)
}

func crossNamespacePodAffinityTerm(term corev1.PodAffinityTerm) bool {
	return len(term.Namespaces) != 0 || term.NamespaceSelector != nil
}

func usesCrossNamespacePodAffinity(pod *corev1.Pod) bool {
	if pod.Spec.Affinity == nil {
		return false
	}

	check := func(terms []corev1.PodAffinityTerm, weighted []corev1.WeightedPodAffinityTerm) bool {
		return slices.ContainsFunc(terms, crossNamespacePodAffinityTerm) ||
			slices.ContainsFunc(weighted, func(term corev1.WeightedPodAffinityTerm) bool {
				return crossNamespacePodAffinityTerm(term.PodAffinityTerm)
			})
	}

	if affinity := pod.Spec.Affinity.PodAffinity; affinity != nil &&
		check(affinity.RequiredDuringSchedulingIgnoredDuringExecution, affinity.PreferredDuringSchedulingIgnoredDuringExecution) {
		return true
	}

	if affinity := pod.Spec.Affinity.PodAntiAffinity; affinity != nil &&
		check(affinity.RequiredDuringSchedulingIgnoredDuringExecution, affinity.PreferredDuringSchedulingIgnoredDuringExecution) {
		return true
	}

	return false
}
