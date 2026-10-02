// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func BenchmarkRootFilesystemMutation(b *testing.B) {
	for _, size := range []int{1, 20} {
		for _, tenants := range []int{1, 4} {
			for _, mode := range []string{"create", "ephemeral", "skip-target", "skip-condition", "condition-error"} {
				b.Run(fmt.Sprintf("rules-and-containers=%d/tenants=%d/%s", size, tenants, mode), func(b *testing.B) {
					compiler, err := cache.NewCELCache()
					require.NoError(b, err)
					h := MetadataRules(compiler)
					old, obj := ephemeralRootFilesystemObjects(b)
					pod := &corev1.Pod{}
					require.NoError(b, runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, pod))
					for i := 1; i < size; i++ {
						pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: fmt.Sprintf("app-%d", i)})
						pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: fmt.Sprintf("debug-%d", i)}})
					}
					pod.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem = nil
					if mode == "create" {
						pod.Spec.EphemeralContainers = nil
					}
					obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
					require.NoError(b, err)
					operation, subresource := admissionv1.Update, "ephemeralcontainers"
					if mode == "create" {
						operation, subresource = admissionv1.Create, ""
					}
					req := rootFilesystemAdmissionRequest(b, obj, old, operation, subresource)
					profiles := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					for tenant := range profiles {
						for range size {
							body := rootFilesystemBody(new(tenant%2 == 0), rules.ValidatePod)
							switch mode {
							case "skip-target":
								body.Mutate[0].Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateInitContainers}
							case "skip-condition":
								body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "false"}}
							case "condition-error":
								body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "object.spec.missing == true"}}
							}
							profiles[tenant] = append(profiles[tenant], body)
						}
					}
					b.ReportAllocs()
					for i := 0; b.Loop(); i++ {
						// Include request object copying, as each admission operates on a fresh object.
						current := obj.DeepCopy()
						response := h.OnUpdate(nil, nil, old, current, nil, nil, nil, profiles[i%tenants])(b.Context(), req)
						switch mode {
						case "skip-target", "skip-condition":
							if response != nil {
								b.Fatal("expected skip")
							}
						case "condition-error":
							if response == nil || response.Allowed {
								b.Fatal("expected failed condition")
							}
						default:
							if response == nil || !response.Allowed || len(response.Patches) == 0 {
								b.Fatal("expected root filesystem patch")
							}
						}
					}
				})
			}
		}
	}
}
