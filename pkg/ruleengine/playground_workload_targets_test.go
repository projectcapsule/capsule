// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	genericmutation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/mutation"
	genericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	podvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/pods/validation"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TestPlaygroundWorkloadTargets(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	tnt := &capsulev1beta2.Tenant{}
	data, err := os.ReadFile("../../playground/platform/tenants/solar.yaml")
	require.NoError(t, err)
	require.NoError(t, yaml.UnmarshalStrict(data, tnt))
	compiler := conditionCache(t)
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	for _, profile := range []string{"test", "prod"} {
		for _, example := range []string{"deployment", "daemonset-denied", "deployment-denied"} {
			t.Run(profile+"/"+example, func(t *testing.T) {
				ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "solar-" + profile, Labels: map[string]string{"env": profile}}}
				bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
				require.NoError(t, err)
				fixture := example
				if example == "deployment-denied" {
					fixture = "deployment"
				}
				data, err := os.ReadFile("../../playground/user/solar/placement/" + fixture + ".yaml")
				require.NoError(t, err)
				obj := &unstructured.Unstructured{}
				require.NoError(t, yaml.Unmarshal(data, &obj.Object))
				obj.SetNamespace(ns.Name)
				if example == "deployment-denied" {
					containers, _, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
					require.NoError(t, err)
					containers[0].(map[string]any)["image"] = "example.com/blocked/app:v1"
					require.NoError(t, unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"))
				}
				raw, err := json.Marshal(obj)
				require.NoError(t, err)
				req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Namespace: ns.Name, Kind: metav1.GroupVersionKind(obj.GroupVersionKind()), Object: runtime.RawExtension{Raw: raw}}}
				response := genericmutation.MetadataRules(compiler).OnCreate(nil, nil, obj, nil, recorder, tnt, bodies)(t.Context(), req)
				if response != nil {
					require.True(t, response.Allowed)
				}
				metadata := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: obj.GetName(), Namespace: ns.Name}}
				response = genericvalidation.GenericRules(nil, compiler).OnCreate(nil, nil, metadata, nil, recorder, tnt, bodies)(t.Context(), req)
				if response == nil {
					response = podvalidation.TemplateRules(nil, nil, compiler).OnCreate(nil, nil, obj, nil, recorder, tnt, bodies)(t.Context(), req)
				}
				if profile == "test" && example != "deployment" {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					if example == "daemonset-denied" {
						require.Contains(t, response.Result.Message, "workload type")
					} else {
						require.Contains(t, response.Result.Message, "registry")
					}
				} else {
					require.Nil(t, response)
				}
				if example == "deployment" {
					pod, err := workloads.PodFromTemplate(obj)
					require.NoError(t, err)
					if profile == "test" {
						require.Equal(t, "32Mi", pod.Spec.Containers[0].Resources.Requests.Memory().String())
					} else {
						require.Empty(t, pod.Spec.Containers[0].Resources.Requests)
					}
				}
			})
		}
	}
}
