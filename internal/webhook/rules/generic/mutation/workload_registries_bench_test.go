// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func BenchmarkImagePullSecretsMutation(b *testing.B) {
	for _, size := range []int{1, 32} {
		for _, tenants := range []int{1, 4} {
			for _, mode := range []string{"merge", "merge-duplicates", "replace", "clear", "skip-condition", "condition-error", "skip-update"} {
				b.Run(fmt.Sprintf("rules-and-secrets=%d/tenants=%d/%s", size, tenants, mode), func(b *testing.B) {
					compiler, err := cache.NewCELCache()
					require.NoError(b, err)
					h := MetadataRules(compiler)
					old, obj := ephemeralRootFilesystemObjects(b)
					unstructured.RemoveNestedField(obj.Object, "spec", "ephemeralContainers")
					existing := make([]any, size)
					for i := range existing {
						existing[i] = map[string]any{"name": fmt.Sprintf("existing-%d", i)}
					}
					if mode == "merge-duplicates" {
						existing = append(existing, existing...)
					}
					require.NoError(b, unstructured.SetNestedSlice(obj.Object, existing, "spec", "imagePullSecrets"))
					operation := admissionv1.Create
					if mode == "skip-update" {
						operation = admissionv1.Update
					}
					req := rootFilesystemAdmissionRequest(b, obj, old, operation, "")
					profiles := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					for tenant := range profiles {
						names := make([]string, size)
						for i := range names {
							names[i] = fmt.Sprintf("tenant-%d-pull-%d", tenant, i)
						}
						for range size {
							body := imagePullSecretsBody(rules.MutationActionMerge, pullSecretRefs(names...), rules.ValidatePod)
							switch mode {
							case "replace":
								body.Mutate[0].Action = rules.MutationActionReplace
							case "clear":
								body.Mutate[0].Action = rules.MutationActionReplace
								body.Mutate[0].Workloads.Registries.ImagePullSecrets = pullSecretRefs()
							case "skip-condition":
								body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "false"}}
							case "condition-error":
								body.Mutate[0].Conditions = []rules.AdmissionCondition{{Expression: "object.missing == true"}}
							}
							profiles[tenant] = append(profiles[tenant], body)
						}
					}
					b.ReportAllocs()
					for i := 0; b.Loop(); i++ {
						current := obj.DeepCopy()
						response := h.OnUpdate(nil, nil, old, current, nil, nil, nil, profiles[i%tenants])(b.Context(), req)
						switch mode {
						case "skip-condition", "skip-update":
							if response != nil {
								b.Fatal("expected skip")
							}
						case "condition-error":
							if response == nil || response.Allowed {
								b.Fatal("expected condition failure")
							}
						default:
							if response == nil || !response.Allowed || len(response.Patches) == 0 {
								b.Fatal("expected secret reference mutation")
							}
							value, _, err := unstructured.NestedFieldNoCopy(current.Object, "spec", "imagePullSecrets")
							if err != nil {
								b.Fatal(err)
							}
							values, _ := value.([]any)
							want := size
							if mode == "clear" {
								want = 0
							} else if mode == "merge" || mode == "merge-duplicates" {
								want = 2 * size
							}
							if len(values) != want {
								b.Fatalf("got %d secret references, expected %d", len(values), want)
							}
						}
					}
				})
			}
		}
	}
}
