// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	triggers "github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func subresourceFixture(tb testing.TB, unrelated int) (func(context.Context, admission.Request) *admission.Response, context.Context, admission.Request, *notificationRecorder, *atomic.Int64, *atomic.Int64, client.Client) {
	tb.Helper()
	_, _, req, n, cached, parent, c := triggerFixture(tb, unrelated)
	if err := appsv1.AddToScheme(c.Scheme()); err != nil {
		tb.Fatal(err)
	}
	parent.Spec.Triggers[0].APIGroups = []string{"apps"}
	parent.Spec.Triggers[0].Resources = []string{"deployments/scale"}
	if err := c.Update(tb.Context(), parent); err != nil {
		tb.Fatal(err)
	}
	req.Name = "workload"
	req.SubResource = "scale"
	req.Resource = metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	req.Kind = metav1.GroupVersionKind{Group: "autoscaling", Version: "v1", Kind: "Scale"}
	req.Object.Raw = []byte(`{"apiVersion":"autoscaling/v1","kind":"Scale","metadata":{"name":"workload","namespace":"team-a","uid":"workload-uid"},"spec":{"replicas":2}}`)
	req.OldObject = req.Object
	direct := &atomic.Int64{}
	reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		direct.Add(1)
		return c.Get(ctx, key, obj, opts...)
	}})
	h := TriggerHandler(nil, n, logr.Discard())
	cached.Store(0)
	return h.OnUpdate(c, reader, admission.NewDecoder(c.Scheme()), nil), authenticatedContext(tb, triggers.MatchID(&parent.Spec.Triggers[0])), req, n, cached, direct, c
}

func TestTriggerSubresourceParentManagement(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		labels                           map[string]string
		uid                              string
		missing, fail, foreign, audience bool
		want, gets                       int64
	}{
		{name: "unmanaged", uid: "workload-uid", want: 1, gets: 1},
		{name: "managed", labels: map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueControllerReplications}, uid: "workload-uid", gets: 1},
		{name: "recreated-parent", uid: "different", gets: 1},
		{name: "missing-parent", missing: true, gets: 1},
		{name: "reader-error", fail: true, uid: "workload-uid", gets: 1},
		{name: "other-namespace", foreign: true, uid: "workload-uid", gets: 1},
		{name: "audience-mismatch", audience: true, uid: "workload-uid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, ctx, req, n, cached, direct, c := subresourceFixture(t, 0)
			deployment := &appsv1.Deployment{Name: req.Name, Namespace: req.Namespace, UID: types.UID(tc.uid), Labels: tc.labels}
			if tc.foreign {
				deployment.Namespace = "team-b"
			}
			if !tc.missing {
				if err := c.Create(t.Context(), deployment); err != nil {
					t.Fatal(err)
				}
			}
			if tc.fail {
				h := TriggerHandler(nil, n, logr.Discard())
				broken := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					direct.Add(1)
					return errors.New("unavailable")
				}})
				run = h.OnUpdate(c, broken, admission.NewDecoder(c.Scheme()), nil)
			}
			if tc.audience {
				req.UserInfo.Username = "bob"
			}
			cached.Store(0)
			if run(ctx, req) != nil || n.calls.Load() != tc.want || direct.Load() != tc.gets {
				t.Fatalf("notifications=%d reads=%d want notifications=%d reads=%d", n.calls.Load(), direct.Load(), tc.want, tc.gets)
			}
			if cached.Load() != 2+tc.gets && !tc.fail {
				t.Fatalf("unexpected cached/read calls=%d", cached.Load())
			}
		})
	}
}

func BenchmarkTriggerSubresourceParent(b *testing.B) {
	for _, unrelated := range []int{0, 1000} {
		for _, managed := range []bool{false, true} {
			b.Run(fmt.Sprintf("unrelated=%d/managed=%t", unrelated, managed), func(b *testing.B) {
				run, ctx, req, n, _, direct, c := subresourceFixture(b, unrelated)
				labels := map[string]string{}
				if managed {
					labels[meta.CreatedByCapsuleLabel] = meta.ValueController
				}
				deployment := &appsv1.Deployment{Name: req.Name, Namespace: req.Namespace, UID: "workload-uid", Labels: labels}
				if err := c.Create(b.Context(), deployment); err != nil {
					b.Fatal(err)
				}
				// Ensure the projection itself contains no management information.
				var projected metav1.PartialObjectMetadata
				if err := json.Unmarshal(req.Object.Raw, &projected); err != nil || len(projected.Labels) != 0 {
					b.Fatal("invalid scale projection")
				}
				b.ReportAllocs()
				for b.Loop() {
					if run(ctx, req) != nil {
						b.Fatal("terminated admission")
					}
				}
				if managed && n.calls.Load() != 0 || !managed && n.calls.Load() != int64(b.N) {
					b.Fatal("incorrect notifications")
				}
				b.ReportMetric(float64(direct.Load())/float64(b.N), "parent-GET/op")
			})
		}
	}
}

func TestTriggerParentReadIsFreshAndShared(t *testing.T) {
	_, ctx, req, n, _, _, c := subresourceFixture(t, 0)
	current := &appsv1.Deployment{Name: req.Name, Namespace: req.Namespace, UID: "workload-uid"}
	if err := c.Create(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	locals := &capsulev1beta2.TenantResourceList{}
	if err := c.List(t.Context(), locals, client.InNamespace(req.Namespace)); err != nil {
		t.Fatal(err)
	}
	global := &capsulev1beta2.GlobalTenantResource{Name: "second", UID: "second-uid", Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: locals.Items[0].Spec.TenantResourceCommonSpec}}
	if err := c.Create(t.Context(), global); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int64
	var managed atomic.Bool
	reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(_ context.Context, _ client.WithWatch, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
		reads.Add(1)
		if key.Namespace != req.Namespace || key.Name != req.Name {
			t.Fatal("unscoped parent lookup")
		}
		parent := obj.(*metav1.PartialObjectMetadata)
		parent.UID = "workload-uid"
		if managed.Load() {
			parent.Labels = map[string]string{meta.CreatedByCapsuleLabel: meta.ValueController}
		}
		return nil
	}})
	run := TriggerHandler(nil, n, logr.Discard()).OnUpdate(c, reader, admission.NewDecoder(c.Scheme()), nil)
	managed.Store(true)
	if run(ctx, req) != nil || n.calls.Load() != 0 || reads.Load() != 1 {
		t.Fatal("cached unmanaged parent bypassed current management metadata")
	}
	managed.Store(false)
	reads.Store(0)
	if run(ctx, req) != nil || n.calls.Load() != 2 || reads.Load() != 1 {
		t.Fatal("parent read not shared across applicable targets")
	}
}
