// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// classBenchCase builds the tenants, rules and request of one admission
// benchmark case, and reports whether the request is denied.
func classBenchCase(b *testing.B, tenants, count int, mode string) (*classClient, admission.Decoder, admission.Request, bool) {
	b.Helper()

	body := allowClassNames("silver")
	body.Enforce.Storage.AttributeClasses = append(body.Enforce.Storage.AttributeClasses, rules.StorageClassMatch{Expression: "^csi-"})

	switch mode {
	case "allow-name-selector", "allow-selector":
		body.Enforce.Storage.AttributeClasses = append(body.Enforce.Storage.AttributeClasses, customerClasses)
	case "no-class-rules":
		body = &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Storage: rules.NamespaceRuleEnforceStorageBody{
			Volumes: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{}}},
		}}}
	}

	bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
	for i := range bodies {
		bodies[i] = body.DeepCopy()
	}

	profiles := []classProfile{{tenant: "tenant-0", namespace: "tenant-0", rules: bodies, status: true}}
	for i := 1; i < tenants; i++ {
		name := fmt.Sprintf("unrelated-%d", i)
		profiles = append(profiles, classProfile{tenant: name, namespace: name, rules: []*rules.NamespaceRuleBodyNamespace{denyClassNames("silver")}, status: true})
	}

	cl, decoder := classFixture(b, profiles...)

	class, denied := new("silver"), false
	switch mode {
	case "allow-selector":
		class = new("gold")
	case "deny":
		class, denied = new("bronze"), true
	case "no-class":
		class = nil
	}

	req := claimRequest(b, newClaim("tenant-0", class), nil)
	switch mode {
	case "unchanged-update":
		req = claimRequest(b, newClaim("tenant-0", new("bronze"), bound(new("bronze")), func(obj *corev1.PersistentVolumeClaim) { obj.Labels = map[string]string{"retention": "weekly"} }), newClaim("tenant-0", new("bronze"), bound(new("bronze"))))
	case "restore-applied":
		req = claimRequest(b, newClaim("tenant-0", new("bronze"), bound(new("bronze"))), newClaim("tenant-0", new("silver"), bound(new("bronze"))))
	case "other-kind":
		req.Kind = metav1.GroupVersionKind{Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshot"}
	}

	return cl, decoder, req, denied
}

func BenchmarkVolumeAttributesClassAdmission(b *testing.B) {
	modes := []string{"allow-name", "allow-name-selector", "allow-selector", "deny", "no-class", "no-class-rules", "unchanged-update", "restore-applied", "other-kind"}

	for _, tenants := range []int{1, 100} {
		for _, count := range []int{1, 16} {
			for _, mode := range modes {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					cl, decoder, req, denied := classBenchCase(b, tenants, count, mode)

					handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)
					recorder := newRecorder()

					runClaim(b.Context(), handler, cl, decoder, recorder, req)
					cl.gets.Store(0)
					cl.classGets.Store(0)

					b.ReportAllocs()

					for b.Loop() {
						recorder.emitted = recorder.emitted[:0]

						response := runClaim(b.Context(), handler, cl, decoder, recorder, req)
						if (response != nil) != denied {
							b.Fatalf("unexpected response: %#v", response)
						}
					}

					b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "reads/op")
					b.ReportMetric(float64(cl.classGets.Load())/float64(b.N), "class-reads/op")
				})
			}
		}
	}
}

// BenchmarkVolumeAttributesClassAdmissionCaches covers what the warm serial
// cases can't show. Cold builds the handler with empty caches before every
// request, so its expressions and selectors are compiled again. Parallel shares
// one warm handler between goroutines, the way concurrent admissions share its
// caches.
func BenchmarkVolumeAttributesClassAdmissionCaches(b *testing.B) {
	for _, tenants := range []int{1, 100} {
		for _, count := range []int{1, 16} {
			for _, mode := range []string{"allow-name", "allow-selector", "deny"} {
				for _, phase := range []string{"cold", "parallel"} {
					b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s/%s", tenants, count, mode, phase), func(b *testing.B) {
						cl, decoder, req, denied := classBenchCase(b, tenants, count, mode)

						b.ReportAllocs()

						if phase == "cold" {
							for b.Loop() {
								handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)

								response := runClaim(b.Context(), handler, cl, decoder, newRecorder(), req)
								if (response != nil) != denied {
									b.Fatalf("unexpected response: %#v", response)
								}
							}
						} else {
							handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)

							runClaim(b.Context(), handler, cl, decoder, newRecorder(), req)
							cl.gets.Store(0)
							cl.classGets.Store(0)

							b.ResetTimer()
							b.RunParallel(func(pb *testing.PB) {
								recorder := newRecorder()

								for pb.Next() {
									recorder.emitted = recorder.emitted[:0]

									response := runClaim(b.Context(), handler, cl, decoder, recorder, req)
									if (response != nil) != denied {
										b.Errorf("unexpected response: %#v", response)
									}
								}
							})
						}

						b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "reads/op")
						b.ReportMetric(float64(cl.classGets.Load())/float64(b.N), "class-reads/op")
					})
				}
			}
		}
	}
}

// busyPodSpec is a Pod spec of a realistic size without claim templates, so
// that the prefilter has to scan a full payload.
func busyPodSpec(volumes ...corev1.Volume) corev1.PodSpec {
	spec := corev1.PodSpec{Volumes: append([]corev1.Volume{
		{Name: "config", ConfigMap: &corev1.ConfigMapVolumeSource{Name: "config"}},
		emptyDir("tmp"),
	}, volumes...)}

	for c := range 3 {
		container := corev1.Container{Name: fmt.Sprintf("app-%d", c), Image: fmt.Sprintf("registry.example.com/team/app-%d:v1.2.3", c),
			VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/etc/app"}, {Name: "tmp", MountPath: "/tmp"}}}
		for e := range 15 {
			container.Env = append(container.Env, corev1.EnvVar{Name: fmt.Sprintf("APP_SETTING_%02d", e), Value: fmt.Sprintf("value-%02d-for-container-%d", e, c)})
		}

		spec.Containers = append(spec.Containers, container)
	}

	return spec
}

// templateBenchCase builds the request of one claim template benchmark case,
// and reports whether it is denied.
func templateBenchCase(b *testing.B, mode string) (admission.Request, bool) {
	b.Helper()

	pod := func(volumes ...corev1.Volume) client.Object {
		obj, _ := workload("Pod", "tenant-0").(*corev1.Pod)
		obj.Spec = busyPodSpec(volumes...)

		return obj
	}
	deployment := func(image string, volumes ...corev1.Volume) client.Object {
		obj, _ := workload("Deployment", "tenant-0").(*appsv1.Deployment)
		obj.Spec.Template.Spec = busyPodSpec(volumes...)
		obj.Spec.Template.Spec.Containers[0].Image = image

		return obj
	}
	claims := func(class string) []corev1.PersistentVolumeClaim {
		return []corev1.PersistentVolumeClaim{claimTemplate("data", new(class)), claimTemplate("logs", new("silver"))}
	}

	switch mode {
	case "pod-no-class":
		return objectRequest(b, pod(), nil), false
	case "deployment-no-class":
		return objectRequest(b, deployment("registry.example.com/team/app:v1"), nil), false
	case "pod-allow":
		return objectRequest(b, pod(ephemeralClaim("scratch", new("silver"))), nil), false
	case "pod-deny":
		return objectRequest(b, pod(ephemeralClaim("scratch", new("bronze"))), nil), true
	case "statefulset-allow":
		return objectRequest(b, statefulSet("tenant-0", claims("csi-fast")), nil), false
	case "statefulset-deny":
		return objectRequest(b, statefulSet("tenant-0", claims("bronze")), nil), true
	case "deployment-rollout":
		return objectRequest(b, deployment("registry.example.com/team/app:v2", ephemeralClaim("scratch", new("bronze"))),
			deployment("registry.example.com/team/app:v1", ephemeralClaim("scratch", new("bronze")))), false
	case "pod-update":
		old := pod(ephemeralClaim("scratch", new("bronze")))
		obj := pod(ephemeralClaim("scratch", new("bronze")))
		obj.SetLabels(map[string]string{"retention": "weekly"})

		return objectRequest(b, obj, old), false
	}

	b.Fatalf("unknown mode %q", mode)

	return admission.Request{}, false
}

func BenchmarkVolumeAttributesClassTemplateAdmission(b *testing.B) {
	modes := []string{"pod-no-class", "deployment-no-class", "pod-allow", "pod-deny", "statefulset-allow", "statefulset-deny", "deployment-rollout", "pod-update"}

	for _, tenants := range []int{1, 100} {
		for _, count := range []int{1, 16} {
			for _, mode := range modes {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					cl, decoder, _, _ := classBenchCase(b, tenants, count, "allow-name")
					req, denied := templateBenchCase(b, mode)

					handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)
					recorder := newRecorder()

					runClaim(b.Context(), handler, cl, decoder, recorder, req)
					cl.gets.Store(0)
					cl.classGets.Store(0)

					b.ReportAllocs()

					for b.Loop() {
						recorder.emitted = recorder.emitted[:0]

						response := runClaim(b.Context(), handler, cl, decoder, recorder, req)
						if (response != nil) != denied {
							b.Fatalf("unexpected response: %#v", response)
						}
					}

					b.ReportMetric(float64(len(req.Object.Raw)), "request-bytes")
					b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "reads/op")
					b.ReportMetric(float64(cl.classGets.Load())/float64(b.N), "class-reads/op")
				})
			}
		}
	}
}

// BenchmarkVolumeAttributesClassServedVersions measures selector matching on a
// cluster that serves VolumeAttributesClass as v1, only as v1beta1, or not at
// all, once the handler has learned which versions to skip.
func BenchmarkVolumeAttributesClassServedVersions(b *testing.B) {
	for _, served := range []string{"v1", "v1beta1", "none"} {
		b.Run(served, func(b *testing.B) {
			cl, decoder, req, _ := classBenchCase(b, 1, 1, "allow-selector")

			switch served {
			case "v1beta1":
				cl.unserved = []string{"v1"}
			case "none":
				cl.unserved = []string{"v1", "v1beta1"}
			}

			denied := served == "none"
			handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)

			runClaim(b.Context(), handler, cl, decoder, newRecorder(), req)
			cl.gets.Store(0)
			cl.classGets.Store(0)

			b.ReportAllocs()

			for b.Loop() {
				response := runClaim(b.Context(), handler, cl, decoder, newRecorder(), req)
				if (response != nil) != denied {
					b.Fatalf("unexpected response: %#v", response)
				}
			}

			b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "reads/op")
			b.ReportMetric(float64(cl.classGets.Load())/float64(b.N), "class-reads/op")
		})
	}
}
