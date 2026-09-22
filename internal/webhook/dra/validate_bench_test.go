// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	resources "k8s.io/api/resource/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func BenchmarkDeviceClassAdmission(b *testing.B) {
	var classes []string
	for i := range resources.DeviceRequestsMaxSize {
		classes = append(classes, fmt.Sprintf("gpu-many-%d", i))
	}
	for _, tenants := range []int{1, 3, 100} {
		for _, tc := range []struct {
			name      string
			requests  []resources.DeviceRequest
			namespace string
			denied    bool
		}{
			{name: "single", requests: exactRequests("gpu-a")},
			{name: "multiple", requests: exactRequests("gpu-a", "gpu-b")},
			{name: "repeated", requests: exactRequests("gpu-a", "gpu-a", "gpu-a", "gpu-a")},
			{name: "denied", requests: exactRequests("gpu-other"), denied: true},
			{name: "skip", requests: exactRequests("gpu-a"), namespace: "unmanaged"},
			{name: "alternatives", requests: alternativeRequests("gpu-a", "gpu-b")},
			{name: "denied-alternative", requests: alternativeRequests("gpu-a", "gpu-other"), denied: true},
			{name: "max-requests", requests: exactRequests(classes...)},
			{name: "max-alternatives", requests: alternativeRequests(classes[:resources.FirstAvailableDeviceRequestMaxSize]...)},
		} {
			b.Run(fmt.Sprintf("tenants=%d/%s", tenants, tc.name), func(b *testing.B) {
				var extra []client.Object
				for _, name := range classes {
					extra = append(extra, &resources.DeviceClass{Name: name, Labels: map[string]string{"tenant": "a"}})
				}
				for i := 3; i < tenants; i++ {
					extra = append(extra, &capsulev1beta2.Tenant{
						Name:   fmt.Sprintf("unrelated-%d", i),
						Status: capsulev1beta2.TenantStatus{Namespaces: []string{fmt.Sprintf("unrelated-%d", i)}},
					})
				}
				cl := newDRAClient(b, extra...)
				if tenants == 1 {
					require.NoError(b, cl.Delete(b.Context(), &capsulev1beta2.Tenant{Name: "tenant-b"}))
					require.NoError(b, cl.Delete(b.Context(), &capsulev1beta2.Tenant{Name: "unrestricted"}))
				}
				namespace := tc.namespace
				if namespace == "" {
					namespace = "ns-a"
				}
				req := draAdmissionRequest(b, "ResourceClaim", namespace, tc.requests)
				handler := DeviceClass().OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewEventRecorder(nil, logr.Discard(), nil, nil))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					response := handler(b.Context(), req)
					if tc.denied {
						if response == nil || response.Allowed {
							b.Fatal("expected denial")
						}
					} else if response != nil {
						b.Fatalf("unexpected response: %+v", response)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(cl.deviceGets)/float64(b.N), "class-gets/op")
				b.ReportMetric(float64(cl.tenantLists)/float64(b.N), "tenant-lists/op")
			})
		}
	}
}
