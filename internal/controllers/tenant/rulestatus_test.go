// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/lru"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestRuleStatusEventsPreserveDriftRepair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	tnt := &capsulev1beta2.Tenant{Name: "team", UID: "tenant-uid"}
	ns := &corev1.Namespace{Name: "team-test"}
	body := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny}}}
	r := &Manager{ruleStatusWrites: lru.New(2)}
	p := r.ruleStatusChangedPredicate()
	writes := 0
	r.Client = fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&capsulev1beta2.RuleStatus{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				writes++
				if p.Create(event.CreateEvent{Object: obj}) {
					t.Error("own creation enqueues another Tenant pass before write returns")
				}
				return c.Create(ctx, obj, opts...)
			},
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				writes++
				if !r.expectedRuleStatus(obj) {
					t.Error("own update was not recorded before write")
				}
				return c.Update(ctx, obj, opts...)
			},
		}).Build()
	ensure := func() {
		t.Helper()
		if err := r.ensureRuleStatus(ctx, logr.Discard(), tnt, ns, body); err != nil {
			t.Fatal(err)
		}
	}
	ensure()
	ensure()
	if writes != 1 {
		t.Fatalf("unchanged projection caused %d writes, want 1 creation", writes)
	}
	current := &capsulev1beta2.RuleStatus{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, current); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*capsulev1beta2.RuleStatus){
		"spec": func(obj *capsulev1beta2.RuleStatus) {
			obj.Spec[0].Enforce.Action = rules.ActionTypeAllow
			obj.Generation++
		},
		"labels": func(obj *capsulev1beta2.RuleStatus) { obj.Labels[meta.NewTenantLabel] = "wrong" },
		"annotations": func(obj *capsulev1beta2.RuleStatus) {
			obj.Annotations = map[string]string{"example.com/input": "changed"}
		},
		"owner":       func(obj *capsulev1beta2.RuleStatus) { obj.OwnerReferences[0].UID = "different-tenant" },
		"terminating": func(obj *capsulev1beta2.RuleStatus) { now := metav1.Now(); obj.DeletionTimestamp = &now },
	} {
		t.Run(name, func(t *testing.T) {
			changed := current.DeepCopy()
			mutate(changed)
			if !p.Update(event.UpdateEvent{ObjectOld: current, ObjectNew: changed}) {
				t.Fatal("external drift was suppressed")
			}
		})
	}
	statusOnly := current.DeepCopy()
	statusOnly.Status.ObservedGeneration++
	statusOnly.ResourceVersion = "new"
	if p.Update(event.UpdateEvent{ObjectOld: current, ObjectNew: statusOnly}) {
		t.Fatal("status update enqueues Tenant")
	}
	body[0].Enforce.Action = rules.ActionTypeAllow
	ensure()
	updated := &capsulev1beta2.RuleStatus{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(current), updated); err != nil {
		t.Fatal(err)
	}
	updated.Generation++ // fake client does not increment generation on a spec update.
	if p.Update(event.UpdateEvent{ObjectOld: current, ObjectNew: updated}) {
		t.Fatal("own spec update enqueues Tenant")
	}
	if !p.Delete(event.DeleteEvent{Object: updated}) {
		t.Fatal("deletion did not enqueue repair")
	}
	if !p.Create(event.CreateEvent{Object: updated}) {
		t.Fatal("deletion did not invalidate the remembered projection")
	}
	ensure()
	r.ruleStatusWrites.Clear()
	if !p.Create(event.CreateEvent{Object: updated}) {
		t.Fatal("restart/cache miss did not enqueue repair")
	}
}
