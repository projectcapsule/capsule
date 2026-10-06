// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	genericmutation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/mutation"
	podvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/pods/validation"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestPlaygroundPlacementExamples(t *testing.T) {
	tenant := &capsulev1beta2.Tenant{}
	data, err := os.ReadFile("../../playground/platform/tenants/solar.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.UnmarshalStrict(data, tenant); err != nil {
		t.Fatal(err)
	}
	compiler := conditionCache(t)
	bodies := []*rules.NamespaceRuleBodyNamespace{tenant.Spec.Rules[0].NamespaceRuleBodyNamespace}
	if err := ruleengine.ValidateRuleStatusBody(nil, bodies, compiler); err != nil {
		t.Fatal(err)
	}
	if tenant.Spec.Rules[0].NamespaceSelector.MatchLabels["env"] != "test" {
		t.Fatal("placement must select test profile")
	}
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	for _, name := range []string{"default", "shared", "shared-custom", "denied"} {
		t.Run(name, func(t *testing.T) {
			fixture := name
			if name == "denied" {
				fixture = "default"
			}
			if name == "shared-custom" {
				fixture = "shared"
			}
			data, err := os.ReadFile("../../playground/user/solar/placement/" + fixture + ".yaml")
			if err != nil {
				t.Fatal(err)
			}
			pod := &corev1.Pod{}
			if err := yaml.UnmarshalStrict(data, pod); err != nil {
				t.Fatal(err)
			}
			// Admission receives the Kubernetes scheduler default even when omitted in YAML.
			pod.Spec.SchedulerName = corev1.DefaultSchedulerName
			wantScheduler := corev1.DefaultSchedulerName
			if name == "shared" {
				wantScheduler = "solar-shared-scheduler"
			}
			if name == "shared-custom" {
				pod.Spec.SchedulerName = "custom-scheduler"
				wantScheduler = "custom-scheduler"
			}
			if name == "denied" {
				pod.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "windows"}
			}
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
			if err != nil {
				t.Fatal(err)
			}
			obj := &unstructured.Unstructured{Object: object}
			raw, err := json.Marshal(obj)
			if err != nil {
				t.Fatal(err)
			}
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Object: runtime.RawExtension{Raw: raw}}}
			response := genericmutation.MetadataRules(compiler).OnCreate(nil, nil, obj, nil, nil, tenant, bodies)(t.Context(), req)
			if response != nil && !response.Allowed {
				t.Fatalf("mutation failed: %#v", response)
			}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, pod); err != nil {
				t.Fatal(err)
			}
			response = podvalidation.PodRules(nil, nil, compiler).OnCreate(nil, nil, pod, nil, recorder, tenant, bodies)(t.Context(), req)
			if name == "denied" {
				if response == nil || response.Allowed {
					t.Fatal("explicit windows selector must be denied")
				}
				return
			}
			if response != nil {
				t.Fatalf("example denied: %#v", response)
			}
			if pod.Spec.NodeSelector["kubernetes.io/os"] != "linux" {
				t.Fatal("missing linux default")
			}
			if pod.Spec.SchedulerName != wantScheduler {
				t.Fatalf("scheduler=%q want=%q", pod.Spec.SchedulerName, wantScheduler)
			}
			if fixture == "shared" && (pod.Spec.NodeSelector["placement.example.com/pool"] != "shared" || len(pod.Spec.Tolerations) != 1) {
				t.Fatal("missing shared placement")
			}
			if name == "default" && len(pod.Spec.Tolerations) != 0 {
				t.Fatal("shared condition did not skip")
			}
		})
	}
}
