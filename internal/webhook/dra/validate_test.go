// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	resources "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	tenantindex "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
)

func TestDeviceClassRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requests  []resources.DeviceRequest
		namespace string
		policy    *api.SelectorAllowedListSpec
		message   string
		gets      int
	}{
		{name: "single allowed request", requests: exactRequests("gpu-a"), gets: 1},
		{name: "all requests allowed", requests: exactRequests("gpu-a", "gpu-b"), gets: 2},
		{name: "later forbidden request", requests: exactRequests("gpu-a", "gpu-other"), message: "Device Class gpu-other is forbidden", gets: 2},
		{name: "first forbidden request", requests: exactRequests("gpu-other", "gpu-a"), message: "Device Class gpu-other is forbidden", gets: 1},
		{name: "later missing class", requests: exactRequests("gpu-a", "missing"), message: "the selected device class does not exist", gets: 2},
		{name: "repeated class", requests: exactRequests("gpu-a", "gpu-a"), gets: 1},
		{name: "all alternatives allowed", requests: alternativeRequests("gpu-a", "gpu-b"), gets: 2},
		{name: "later forbidden alternative", requests: alternativeRequests("gpu-a", "gpu-other"), message: "Device Class gpu-other is forbidden", gets: 2},
		{name: "first forbidden alternative", requests: alternativeRequests("gpu-other", "gpu-a"), message: "Device Class gpu-other is forbidden", gets: 1},
		{name: "later missing alternative", requests: alternativeRequests("gpu-a", "missing"), message: "the selected device class does not exist", gets: 2},
		{name: "repeated alternative", requests: alternativeRequests("gpu-a", "gpu-a"), gets: 1},
		{name: "exact then alternatives", requests: append(exactRequests("gpu-a"), alternativeRequests("gpu-a", "gpu-other")...), message: "Device Class gpu-other is forbidden", gets: 2},
		{name: "alternatives then exact", requests: append(alternativeRequests("gpu-a", "gpu-b"), exactRequests("gpu-other")...), message: "Device Class gpu-other is forbidden", gets: 3},
		{name: "other tenant allowed", namespace: "ns-b", requests: alternativeRequests("gpu-other"), gets: 1},
		{name: "other tenant denied", namespace: "ns-b", requests: exactRequests("gpu-other", "gpu-a"), message: "Device Class gpu-a is forbidden", gets: 2},
		{name: "unmanaged namespace", namespace: "unmanaged", requests: alternativeRequests("missing")},
		{name: "no policy", namespace: "unrestricted", requests: alternativeRequests("missing")},
		{name: "no device requests"},
		{name: "missing request form", requests: []resources.DeviceRequest{{Name: "gpu"}}, message: "the selected device class does not exist"},
		{name: "empty exact class", requests: exactRequests(""), message: "the selected device class does not exist", gets: 1},
		{name: "empty alternative class", requests: alternativeRequests(""), message: "the selected device class does not exist", gets: 1},
		{
			name: "exact allowlist", requests: exactRequests("gpu-a", "gpu-b"), gets: 2,
			policy: &api.SelectorAllowedListSpec{AllowedListSpec: api.AllowedListSpec{Exact: []string{"gpu-b", "gpu-a"}}},
		},
		{
			name: "exact allowlist rejects other classes", requests: exactRequests("gpu-a", "gpu-other"), gets: 2,
			policy:  &api.SelectorAllowedListSpec{AllowedListSpec: api.AllowedListSpec{Exact: []string{"gpu-a"}}},
			message: "Device Class gpu-other is forbidden",
		},
		{
			name: "regex allowlist", requests: alternativeRequests("gpu-a", "gpu-b"), gets: 2,
			policy: &api.SelectorAllowedListSpec{AllowedListSpec: api.AllowedListSpec{Regex: "^gpu-[ab]$"}},
		},
		{
			name: "regex allowlist rejects other classes", requests: exactRequests("gpu-other"), gets: 1,
			policy:  &api.SelectorAllowedListSpec{AllowedListSpec: api.AllowedListSpec{Regex: "^gpu-[ab]$"}},
			message: "Device Class gpu-other is forbidden",
		},
		{
			name: "name or selector", requests: exactRequests("gpu-a", "gpu-other"), gets: 2,
			policy: &api.SelectorAllowedListSpec{
				AllowedListSpec: api.AllowedListSpec{Exact: []string{"gpu-other"}},
				LabelSelector:   metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "a"}},
			},
		},
		{
			name: "match expressions", requests: alternativeRequests("gpu-a", "gpu-other"), gets: 2,
			policy: &api.SelectorAllowedListSpec{LabelSelector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "tenant", Operator: metav1.LabelSelectorOpIn, Values: []string{"a"}},
			}}},
			message: "Device Class gpu-other is forbidden",
		},
		{
			name: "empty policy preserves match all", requests: exactRequests("gpu-a", "gpu-other"), gets: 2,
			policy: &api.SelectorAllowedListSpec{},
		},
	}

	for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
		t.Run(kind, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					cl := newDRAClient(t)
					if tt.policy != nil {
						tnt := &capsulev1beta2.Tenant{}
						require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tnt))
						tnt.Spec.DeviceClasses = tt.policy.DeepCopy()
						require.NoError(t, cl.Update(t.Context(), tnt))
					}
					namespace := tt.namespace
					if namespace == "" {
						namespace = "ns-a"
					}
					req := draAdmissionRequest(t, kind, namespace, tt.requests)
					original := append([]byte(nil), req.Object.Raw...)
					handler := DeviceClass().OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewEventRecorder(nil, logr.Discard(), nil, nil))
					response := handler(t.Context(), req)
					if tt.message == "" {
						require.Nil(t, response, "allowed requests must continue the admission chain")
					} else {
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						require.Contains(t, response.Result.Message, tt.message)
					}
					require.Equal(t, tt.gets, cl.deviceGets)
					require.Equal(t, 1, cl.tenantLists)
					require.Equal(t, original, req.Object.Raw)
				})
			}
		})
	}
}

func TestDeviceClassDependencyErrors(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
		for _, dependency := range []string{"tenant", "deviceclass"} {
			t.Run(kind+"/"+dependency, func(t *testing.T) {
				t.Parallel()
				cl := newDRAClient(t)
				if dependency == "tenant" {
					cl.listError = errors.New("tenant lookup failed")
				} else {
					cl.getErrorClass = "gpu-b"
				}
				handler := DeviceClass().OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewEventRecorder(nil, logr.Discard(), nil, nil))
				response := handler(t.Context(), draAdmissionRequest(t, kind, "ns-a", exactRequests("gpu-a", "gpu-b")))
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.EqualValues(t, http.StatusInternalServerError, response.Result.Code)
				require.Contains(t, response.Result.Message, "lookup failed")
			})
		}
	}
}

func TestDeviceClassSkipsUnrelatedKinds(t *testing.T) {
	t.Parallel()
	cl := newDRAClient(t)
	handler := DeviceClass().OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), nil)
	response := handler(t.Context(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Kind: "Pod"}}})
	require.Nil(t, response)
	require.Zero(t, cl.tenantLists)
	require.Zero(t, cl.deviceGets)
}

func TestDeviceClassRechecksPolicyAndLabels(t *testing.T) {
	t.Parallel()
	cl := newDRAClient(t)
	handler := DeviceClass().OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewEventRecorder(nil, logr.Discard(), nil, nil))
	req := draAdmissionRequest(t, "ResourceClaim", "ns-a", alternativeRequests("gpu-a", "gpu-b"))
	require.Nil(t, handler(t.Context(), req))

	dc := &resources.DeviceClass{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "gpu-b"}, dc))
	dc.Labels["tenant"] = "b"
	require.NoError(t, cl.Update(t.Context(), dc))
	response := handler(t.Context(), req)
	require.NotNil(t, response)
	require.False(t, response.Allowed)
	require.Contains(t, response.Result.Message, "Device Class gpu-b is forbidden")

	tnt := &capsulev1beta2.Tenant{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tnt))
	tnt.Spec.DeviceClasses.Exact = []string{"gpu-b"}
	require.NoError(t, cl.Update(t.Context(), tnt))
	require.Nil(t, handler(t.Context(), req))

	other := draAdmissionRequest(t, "ResourceClaim", "ns-b", exactRequests("gpu-a"))
	response = handler(t.Context(), other)
	require.NotNil(t, response)
	require.False(t, response.Allowed)
	require.Contains(t, response.Result.Message, "Device Class gpu-a is forbidden")
}

func TestDeviceClassDecodeErrors(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			cl := newDRAClient(t)
			req := draAdmissionRequest(t, kind, "ns-a", nil)
			req.Object.Raw = []byte("invalid json")
			handler := DeviceClass().OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), nil)
			response := handler(t.Context(), req)
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			require.Zero(t, cl.tenantLists)
			require.Zero(t, cl.deviceGets)
		})
	}
}

func exactRequests(classes ...string) []resources.DeviceRequest {
	requests := make([]resources.DeviceRequest, 0, len(classes))
	for i, class := range classes {
		requests = append(requests, resources.DeviceRequest{Name: fmt.Sprintf("request-%d", i), Exactly: &resources.ExactDeviceRequest{DeviceClassName: class}})
	}
	return requests
}

func alternativeRequests(classes ...string) []resources.DeviceRequest {
	request := resources.DeviceRequest{Name: "alternatives"}
	for i, class := range classes {
		request.FirstAvailable = append(request.FirstAvailable, resources.DeviceSubRequest{Name: fmt.Sprintf("alternative-%d", i), DeviceClassName: class})
	}
	return []resources.DeviceRequest{request}
}

func draAdmissionRequest(tb testing.TB, kind, namespace string, requests []resources.DeviceRequest) admission.Request {
	tb.Helper()
	spec := resources.ResourceClaimSpec{Devices: resources.DeviceClaim{Requests: requests}}
	var obj client.Object
	if kind == "ResourceClaim" {
		obj = &resources.ResourceClaim{Spec: spec}
	} else {
		obj = &resources.ResourceClaimTemplate{Spec: resources.ResourceClaimTemplateSpec{Spec: spec}}
	}
	obj.SetName("claim")
	obj.SetNamespace(namespace)
	obj.GetObjectKind().SetGroupVersionKind(resources.SchemeGroupVersion.WithKind(kind))
	raw, err := json.Marshal(obj)
	require.NoError(tb, err)
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Kind:      metav1.GroupVersionKind{Group: resources.GroupName, Version: "v1", Kind: kind},
		Namespace: namespace, Name: "claim", Operation: admissionv1.Create,
		Object: runtime.RawExtension{Raw: raw},
	}}
}

func newDRAClient(tb testing.TB, extra ...client.Object) *draCountingClient {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, capsulev1beta2.AddToScheme(scheme))
	require.NoError(tb, resources.AddToScheme(scheme))
	objects := []client.Object{
		&capsulev1beta2.Tenant{Name: "tenant-a", Spec: capsulev1beta2.TenantSpec{DeviceClasses: &api.SelectorAllowedListSpec{
			LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "a"}},
		}}, Status: capsulev1beta2.TenantStatus{Namespaces: []string{"ns-a"}}},
		&capsulev1beta2.Tenant{Name: "tenant-b", Spec: capsulev1beta2.TenantSpec{DeviceClasses: &api.SelectorAllowedListSpec{
			LabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "b"}},
		}}, Status: capsulev1beta2.TenantStatus{Namespaces: []string{"ns-b"}}},
		&capsulev1beta2.Tenant{Name: "unrestricted", Status: capsulev1beta2.TenantStatus{Namespaces: []string{"unrestricted"}}},
		&resources.DeviceClass{Name: "gpu-a", Labels: map[string]string{"tenant": "a"}},
		&resources.DeviceClass{Name: "gpu-b", Labels: map[string]string{"tenant": "a"}},
		&resources.DeviceClass{Name: "gpu-other", Labels: map[string]string{"tenant": "b"}},
	}
	index := tenantindex.NamespacesReference{}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objects, extra...)...).
		WithIndex(&capsulev1beta2.Tenant{}, index.Field(), index.Func()).Build()
	return &draCountingClient{Client: cl}
}

type draCountingClient struct {
	client.Client
	deviceGets    int
	tenantLists   int
	getErrorClass string
	listError     error
}

func (c *draCountingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*resources.DeviceClass); ok {
		c.deviceGets++
		if c.getErrorClass != "" && key.Name == c.getErrorClass {
			return errors.New("device class lookup failed")
		}
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *draCountingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.tenantLists++
	if c.listError != nil {
		return c.listError
	}
	return c.Client.List(ctx, list, opts...)
}
