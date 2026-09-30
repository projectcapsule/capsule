// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestForbiddenServiceMetadataRegex(t *testing.T) {
	t.Parallel()
	handler := Validating()
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	for _, field := range []string{"annotations", "labels"} {
		for _, tc := range []struct{ expression, key, want string }{
			{"[", "example.com/foo", "invalid forbidden metadata regex"},
			{"^blocked-", "blocked-key", "blocked-key is forbidden"},
			{"^blocked-", "safe-key", ""},
			{"", "safe-key", ""},
			{" blocked-", "blocked-key", ""},
		} {
			t.Run(field+"/"+tc.expression+"/"+tc.key, func(t *testing.T) {
				tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}, Spec: capsulev1beta2.TenantSpec{ServiceOptions: &api.ServiceOptions{}}}
				svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "tenant-a-ns"}}
				if field == "annotations" {
					tnt.Spec.ServiceOptions.ForbiddenAnnotations.Regex = tc.expression
					svc.Annotations = map[string]string{tc.key: "value"}
				} else {
					tnt.Spec.ServiceOptions.ForbiddenLabels.Regex = tc.expression
					svc.Labels = map[string]string{tc.key: "value"}
				}
				before := tnt.DeepCopy()
				for _, response := range []*admission.Response{
					handler.OnCreate(nil, nil, svc, nil, recorder, tnt, nil)(t.Context(), admission.Request{}),
					handler.OnUpdate(nil, nil, &corev1.Service{}, svc, nil, recorder, tnt, nil)(t.Context(), admission.Request{}),
				} {
					if tc.want == "" {
						require.Nil(t, response, "allow must continue the admission chain")
					} else {
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						require.EqualValues(t, 403, response.Result.Code)
						require.Contains(t, response.Result.Message, field+" validation failed")
						require.Contains(t, response.Result.Message, tc.want)
					}
				}
				require.Equal(t, before, tnt)
				other := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-b"}}
				require.Nil(t, handler.OnCreate(nil, nil, svc, nil, recorder, other, nil)(t.Context(), admission.Request{}))
				require.Nil(t, handler.OnDelete(nil, nil, svc, nil, recorder, tnt, nil)(t.Context(), admission.Request{}))
			})
		}
	}
}

func BenchmarkForbiddenServiceMetadata(b *testing.B) {
	benchmarkForbiddenServiceMetadata(b, []string{"skip", "allow", "deny"})
}

func BenchmarkInvalidForbiddenServiceMetadata(b *testing.B) {
	benchmarkForbiddenServiceMetadata(b, []string{"invalid"})
}

func benchmarkForbiddenServiceMetadata(b *testing.B, modes []string) {
	b.Helper()
	handler := Validating()
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	for _, tenants := range []int{1, 8} {
		for _, keys := range []int{1, 32} {
			for _, mode := range modes {
				b.Run(fmt.Sprintf("tenants=%d/keys=%d/%s", tenants, keys, mode), func(b *testing.B) {
					tnts := make([]*capsulev1beta2.Tenant, tenants)
					services := make([]*corev1.Service, tenants)
					for i := range tnts {
						tnts[i] = &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("tenant-%d", i)}}
						services[i] = &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: tnts[i].Name, Annotations: map[string]string{}}}
						if mode != "skip" {
							tnts[i].Spec.ServiceOptions = &api.ServiceOptions{ForbiddenAnnotations: api.ForbiddenListSpec{Regex: "^blocked-"}}
						}
						if mode == "invalid" {
							tnts[i].Spec.ServiceOptions.ForbiddenAnnotations.Regex = "["
						}
						for key := range keys {
							services[i].Annotations[fmt.Sprintf("safe-%d", key)] = "value"
						}
						if mode == "deny" {
							services[i].Annotations["blocked-key"] = "value"
						}
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; b.Loop(); i++ {
						response := handler.OnCreate(nil, nil, services[i%tenants], nil, recorder, tnts[i%tenants], nil)(b.Context(), admission.Request{})
						if mode == "deny" || mode == "invalid" {
							if response == nil || response.Allowed {
								b.Fatal("expected denial")
							}
						} else if response != nil {
							b.Fatal(response)
						}
					}
				})
			}
		}
	}
}
