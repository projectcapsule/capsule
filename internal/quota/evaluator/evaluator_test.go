// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package evaluator

import (
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestEvaluatePodUsesUpstreamResourceCalculation(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			}},
			InitContainers: []corev1.Container{{
				Name: "init",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("3"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("4"),
					},
				},
			}},
			Overhead: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("100m"),
			},
		},
	}

	result, handled, err := Evaluate(requestFor(t, admissionv1.Create, "pods", "Pod", pod, nil))
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !handled {
		t.Fatal("Evaluate() did not handle Pod")
	}

	assertQuantity(t, result.NewUsage, corev1.ResourceRequestsCPU, "3100m")
	assertQuantity(t, result.NewUsage, corev1.ResourceLimitsCPU, "4100m")
	assertQuantity(t, result.NewUsage, corev1.ResourceRequestsMemory, "1Gi")
	assertQuantity(t, result.NewUsage, corev1.ResourceLimitsMemory, "2Gi")
	assertQuantity(t, result.NewUsage, corev1.ResourcePods, "1")
	assertQuantity(t, result.NewUsage, corev1.ResourceName("count/pods"), "1")
}

func TestEvaluatePodUsesEphemeralStorageResources(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: resource.MustParse("2Gi"),
				},
			},
		}},
		InitContainers: []corev1.Container{{
			Name: "init",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: resource.MustParse("3Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: resource.MustParse("4Gi"),
				},
			},
		}},
		Overhead: corev1.ResourceList{
			corev1.ResourceEphemeralStorage: resource.MustParse("500Mi"),
		},
	}}

	result, handled, err := Evaluate(requestFor(t, admissionv1.Create, "pods", "Pod", pod, nil))
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !handled {
		t.Fatal("Evaluate() did not handle Pod")
	}

	assertQuantity(t, result.NewUsage, corev1.ResourceEphemeralStorage, "3572Mi")
	assertQuantity(t, result.NewUsage, corev1.ResourceRequestsEphemeralStorage, "3572Mi")
	assertQuantity(t, result.NewUsage, corev1.ResourceLimitsEphemeralStorage, "4596Mi")
}

func TestEvaluateTerminalPodOnlyConsumesObjectCount(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	result, handled, err := Evaluate(requestFor(t, admissionv1.Create, "pods", "Pod", pod, nil))
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !handled {
		t.Fatal("Evaluate() did not handle Pod")
	}

	assertQuantity(t, result.NewUsage, corev1.ResourceName("count/pods"), "1")
	for _, name := range []corev1.ResourceName{
		corev1.ResourcePods,
		corev1.ResourceEphemeralStorage,
		corev1.ResourceRequestsEphemeralStorage,
	} {
		if _, found := result.NewUsage[name]; found {
			t.Fatalf("terminal Pod unexpectedly consumed %q", name)
		}
	}
}

func TestEvaluatePodUsesPodLevelResourceRequests(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "app"}},
		Resources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("600m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("800m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}}

	result, handled, err := Evaluate(requestFor(t, admissionv1.Create, "pods", "Pod", pod, nil))
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !handled {
		t.Fatal("Evaluate() did not handle Pod")
	}

	assertQuantity(t, result.NewUsage, corev1.ResourceRequestsCPU, "600m")
	assertQuantity(t, result.NewUsage, corev1.ResourceRequestsMemory, "512Mi")
	assertQuantity(t, result.NewUsage, corev1.ResourceLimitsCPU, "800m")
	assertQuantity(t, result.NewUsage, corev1.ResourceLimitsMemory, "1Gi")
}

func TestValidateConstraintsAllowsPodLevelResources(t *testing.T) {
	t.Parallel()

	hard := corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("8"),
		corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
		corev1.ResourceLimitsCPU:      resource.MustParse("8"),
		corev1.ResourceLimitsMemory:   resource.MustParse("16Gi"),
	}
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{
			Name:      "nginx",
			Resources: corev1.ResourceRequirements{},
		}},
		Resources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}}

	if err := ValidateConstraints(hard, pod); err != nil {
		t.Fatalf("ValidateConstraints() rejected Pod-level resources: %v", err)
	}
}

func TestValidateConstraintsDoesNotTreatUnsupportedPodLevelResourcesAsCompute(t *testing.T) {
	t.Parallel()

	hard := corev1.ResourceList{
		corev1.ResourceRequestsCPU: resource.MustParse("8"),
	}
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{
			Name:      "nginx",
			Resources: corev1.ResourceRequirements{},
		}},
		Resources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
			},
		},
	}}

	err := ValidateConstraints(hard, pod)
	if err == nil {
		t.Fatal("ValidateConstraints() accepted unsupported Pod-level resources")
	}
	if got, want := err.Error(), "must specify requests.cpu for: nginx"; got != want {
		t.Fatalf("ValidateConstraints() error = %q, want %q", got, want)
	}
}

func TestEvaluateServiceUpdateReturnsOldAndNewUsage(t *testing.T) {
	t.Parallel()

	oldService := &corev1.Service{Spec: corev1.ServiceSpec{
		Type:  corev1.ServiceTypeClusterIP,
		Ports: []corev1.ServicePort{{Port: 80}},
	}}
	newService := oldService.DeepCopy()
	newService.Spec.Type = corev1.ServiceTypeLoadBalancer
	newService.Spec.Ports = append(newService.Spec.Ports, corev1.ServicePort{Port: 443})

	result, handled, err := Evaluate(requestFor(t, admissionv1.Update, "services", "Service", newService, oldService))
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !handled {
		t.Fatal("Evaluate() did not handle Service")
	}

	assertQuantity(t, result.OldUsage, corev1.ResourceServicesLoadBalancers, "0")
	assertQuantity(t, result.OldUsage, corev1.ResourceServices, "1")
	assertQuantity(t, result.OldUsage, corev1.ResourceName("count/services"), "1")
	assertQuantity(t, result.NewUsage, corev1.ResourceServicesLoadBalancers, "1")
	assertQuantity(t, result.NewUsage, corev1.ResourceServicesNodePorts, "2")
}

func TestEvaluateObjectCountNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		group      string
		version    string
		resource   string
		kind       string
		expected   corev1.ResourceName
		legacyName corev1.ResourceName
	}{
		{
			name:       "core resource has generic and legacy count",
			version:    "v1",
			resource:   "configmaps",
			kind:       "ConfigMap",
			expected:   corev1.ResourceName("count/configmaps"),
			legacyName: corev1.ResourceConfigMaps,
		},
		{
			name:     "grouped resource has qualified generic count",
			group:    "apps",
			version:  "v1",
			resource: "deployments",
			kind:     "Deployment",
			expected: corev1.ResourceName("count/deployments.apps"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			object := &metav1.PartialObjectMetadata{
				ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "tenant-a"},
			}
			req := requestFor(t, admissionv1.Create, test.resource, test.kind, object, nil)
			req.Resource = metav1.GroupVersionResource{
				Group: test.group, Version: test.version, Resource: test.resource,
			}
			req.Kind = metav1.GroupVersionKind{
				Group: test.group, Version: test.version, Kind: test.kind,
			}

			result, handled, err := Evaluate(req)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if !handled {
				t.Fatalf("Evaluate() did not handle %s", test.kind)
			}

			assertQuantity(t, result.NewUsage, test.expected, "1")
			if test.legacyName != "" {
				assertQuantity(t, result.NewUsage, test.legacyName, "1")
			}
		})
	}
}

func TestEvaluateCountsHorizontalPodAutoscalers(t *testing.T) {
	t.Parallel()

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "tenant-a"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "example",
			},
			MaxReplicas: 3,
		},
	}
	req := requestFor(
		t,
		admissionv1.Create,
		"horizontalpodautoscalers",
		"HorizontalPodAutoscaler",
		hpa,
		nil,
	)
	req.Resource = metav1.GroupVersionResource{
		Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers",
	}
	req.Kind = metav1.GroupVersionKind{
		Group: "autoscaling", Version: "v2", Kind: "HorizontalPodAutoscaler",
	}

	result, handled, err := Evaluate(req)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !handled {
		t.Fatal("Evaluate() did not handle HorizontalPodAutoscaler")
	}

	assertQuantity(
		t,
		result.NewUsage,
		corev1.ResourceName("count/horizontalpodautoscalers.autoscaling"),
		"1",
	)
}

func TestMatchesPodQuotaScopes(t *testing.T) {
	t.Parallel()

	priority := "high"
	pod := &corev1.Pod{Spec: corev1.PodSpec{PriorityClassName: priority}}
	spec := corev1.ResourceQuotaSpec{ScopeSelector: &corev1.ScopeSelector{
		MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
			ScopeName: corev1.ResourceQuotaScopePriorityClass,
			Operator:  corev1.ScopeSelectorOpIn,
			Values:    []string{"high"},
		}},
	}}

	matches, err := MatchesScopes(spec, pod)
	if err != nil {
		t.Fatalf("MatchesScopes() error = %v", err)
	}
	if !matches {
		t.Fatal("MatchesScopes() = false, want true")
	}
}

func TestMatchesBestEffortScopeUsesPodLevelResources(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Resources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}}
	spec := corev1.ResourceQuotaSpec{Scopes: []corev1.ResourceQuotaScope{
		corev1.ResourceQuotaScopeBestEffort,
	}}

	matches, err := MatchesScopes(spec, pod)
	if err != nil {
		t.Fatalf("MatchesScopes() error = %v", err)
	}
	if matches {
		t.Fatal("pod-level resources were classified as BestEffort")
	}
}

func requestFor(
	t *testing.T,
	operation admissionv1.Operation,
	resourceName string,
	kind string,
	object runtime.Object,
	old runtime.Object,
) admission.Request {
	t.Helper()

	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}

	var oldRaw []byte
	if old != nil {
		oldRaw, err = json.Marshal(old)
		if err != nil {
			t.Fatal(err)
		}
	}

	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: operation,
		Resource:  metav1.GroupVersionResource{Group: "", Version: "v1", Resource: resourceName},
		Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: kind},
		Object:    runtime.RawExtension{Raw: raw},
		OldObject: runtime.RawExtension{Raw: oldRaw},
		RequestKind: &metav1.GroupVersionKind{
			Group: "", Version: "v1", Kind: kind,
		},
		RequestResource: &metav1.GroupVersionResource{
			Group: "", Version: "v1", Resource: resourceName,
		},
	}}
}

func assertQuantity(t *testing.T, list corev1.ResourceList, name corev1.ResourceName, want string) {
	t.Helper()

	got, ok := list[name]
	if !ok {
		t.Fatalf("resource %q is missing from %#v", name, list)
	}
	if got.Cmp(resource.MustParse(want)) != 0 {
		t.Fatalf("resource %q = %s, want %s", name, got.String(), want)
	}
}
