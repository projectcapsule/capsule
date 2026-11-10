// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	registrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	tenantindex "github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	triggers "github.com/projectcapsule/capsule/pkg/runtime/replication"
	runtimewebhook "github.com/projectcapsule/capsule/pkg/runtime/webhook"
)

type notificationRecorder struct {
	calls  atomic.Int64
	target triggers.Target
}

func (n *notificationRecorder) Notify(target triggers.Target, _ time.Duration) bool {
	n.calls.Add(1)
	return true
}

func triggerFixture(tb testing.TB, unrelated int) (handlers.Func, context.Context, admission.Request, *notificationRecorder, *atomic.Int64, *capsulev1beta2.TenantResource, client.Client) {
	tb.Helper()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		tb.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		tb.Fatal(err)
	}
	trigger := capsulev1beta2.TenantResourceTrigger{Name: "secrets", Operations: []registrationv1.OperationType{registrationv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}, Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}}
	local := &capsulev1beta2.TenantResource{Name: "local", Namespace: "team-a", UID: "local-uid", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}}}
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "alice"}}}}
	ns := &corev1.Namespace{Name: "team-a", Labels: map[string]string{meta.TenantLabel: tnt.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}}}
	objects := []client.Object{local, tnt, ns}
	for i := range unrelated {
		other := local.DeepCopy()
		other.Name = fmt.Sprintf("other-%d", i)
		other.Namespace = "team-b"
		other.Spec.Triggers[0].Resources = []string{"pods"}
		objects = append(objects, other)
	}
	calls := &atomic.Int64{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(&capsulev1beta2.TenantResource{}, triggers.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithIndex(&capsulev1beta2.GlobalTenantResource{}, triggers.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithIndex(&capsulev1beta2.Tenant{}, tenantindex.NamespaceIndexerFieldName, (tenantindex.NamespacesReference{}).Func()).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			calls.Add(1)
			return c.Get(ctx, key, obj, opts...)
		},
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			calls.Add(1)
			return c.List(ctx, list, opts...)
		},
	}).Build()
	notifier := &notificationRecorder{}
	h := TriggerHandler(configuration.NewCapsuleConfiguration(tb.Context(), c, c, nil, "capsule"), notifier, logr.Discard())
	object, _ := json.Marshal(&corev1.Secret{Name: "source", Namespace: "team-a"})
	req := admission.Request{Namespace: "team-a", Name: "source", Operation: admissionv1.Update, Resource: metav1.GroupVersionResource{Version: "v1", Resource: "secrets"}, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Secret"}, UserInfo: authenticationv1.UserInfo{Username: "alice"}, Object: runtime.RawExtension{Raw: object}, OldObject: runtime.RawExtension{Raw: object}}
	ctx := runtimewebhook.WithRequestPath(tb.Context(), triggers.TriggerPath+triggers.MatchID(&trigger))
	return h.OnUpdate(c, c, admission.NewDecoder(scheme), nil), ctx, req, notifier, calls, local, c
}

func TestTriggerHandlerScopeAudienceAndSkips(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*admission.Request)
		want  int64
		reads int64
	}{
		{name: "matching", want: 1, reads: 2},
		{name: "audience mismatch", edit: func(r *admission.Request) { r.UserInfo.Username = "bob" }, reads: 2},
		{name: "different namespace", edit: func(r *admission.Request) { r.Namespace = "team-b" }, reads: 2},
		{name: "unregistered custom kind", edit: func(r *admission.Request) {
			r.Object.Raw = []byte(`{"apiVersion":"example.com/v1","kind":"Widget","metadata":{"name":"source","namespace":"team-a"}}`)
			r.OldObject.Raw = r.Object.Raw
		}, want: 1, reads: 2},
		{name: "dry run", edit: func(r *admission.Request) { r.DryRun = new(true) }},
		{name: "cluster scope", edit: func(r *admission.Request) { r.Namespace = "" }},
		{name: "managed new", edit: func(r *admission.Request) {
			r.Object.Raw = []byte(`{"metadata":{"labels":{"projectcapsule.dev/managed-by":"replications"}}}`)
		}},
		{name: "managed old marker removed", edit: func(r *admission.Request) {
			r.OldObject.Raw = []byte(`{"metadata":{"labels":{"projectcapsule.dev/managed-by":"controller"}}}`)
		}},
		{name: "legacy managed", edit: func(r *admission.Request) {
			r.OldObject.Raw = []byte(`{"metadata":{"labels":{"capsule.clastix.io/resources":""}}}`)
		}},
		{name: "malformed", edit: func(r *admission.Request) { r.Object.Raw = []byte(`{`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, ctx, req, n, calls, _, _ := triggerFixture(t, 0)
			if tc.edit != nil {
				tc.edit(&req)
			}
			if response := run(ctx, req); response != nil {
				t.Fatal("notification handler terminated admission chain")
			}
			if n.calls.Load() != tc.want {
				t.Fatalf("notifications=%d want %d", n.calls.Load(), tc.want)
			}
			if calls.Load() != tc.reads {
				t.Fatalf("cached reads=%d want %d", calls.Load(), tc.reads)
			}
		})
	}
}

func TestTriggerHandlerAudienceUpdatesAndObsoletePredicates(t *testing.T) {
	run, ctx, req, n, _, obj, c := triggerFixture(t, 0)
	obj.Spec.Triggers[0].Audience = []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}}
	if err := c.Update(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	run(ctx, req)
	if n.calls.Load() != 1 {
		t.Fatal("source tenant owner did not match")
	}
	req.UserInfo.Username = "owner-of-other-tenant"
	run(ctx, req)
	if n.calls.Load() != 1 {
		t.Fatal("unrelated owner matched")
	}
	obj.Spec.Triggers[0].Resources = []string{"configmaps"}
	if err := c.Update(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	req.UserInfo.Username = "alice"
	run(ctx, req)
	if n.calls.Load() != 1 {
		t.Fatal("obsolete predicate activated updated trigger")
	}
}

func TestSelfSubscriptionSkipsBookkeepingButPreservesUserUpdates(t *testing.T) {
	t.Setenv("SERVICE_ACCOUNT", "capsule")
	t.Setenv("NAMESPACE", "capsule-system")
	run, _, req, n, calls, parent, c := triggerFixture(t, 0)
	parent.Spec.Triggers[0].Audience = nil
	parent.Spec.Triggers[0].APIGroups = []string{capsulev1beta2.GroupVersion.Group}
	parent.Spec.Triggers[0].Resources = []string{"tenantresources"}
	if err := c.Update(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	req.Resource = metav1.GroupVersionResource{Group: capsulev1beta2.GroupVersion.Group, Version: "v1beta2", Resource: "tenantresources"}
	req.Kind = metav1.GroupVersionKind{Group: capsulev1beta2.GroupVersion.Group, Version: "v1beta2", Kind: "TenantResource"}
	req.Name = parent.Name
	var err error
	req.Object.Raw, err = json.Marshal(parent)
	if err != nil {
		t.Fatal(err)
	}
	req.OldObject = req.Object
	ctx := runtimewebhook.WithRequestPath(t.Context(), triggers.TriggerPath+triggers.MatchID(&parent.Spec.Triggers[0]))
	req.UserInfo.Username = "system:serviceaccount:capsule-system:capsule"
	calls.Store(0)
	if response := run(ctx, req); response != nil {
		t.Fatal("bookkeeping terminated admission")
	}
	if n.calls.Load() != 0 || calls.Load() != 0 {
		t.Fatal("self notification queued work or performed lookups")
	}
	req.UserInfo.Username = "alice"
	if response := run(ctx, req); response != nil || n.calls.Load() != 1 {
		t.Fatal("genuine user update was excluded")
	}
}

func BenchmarkTriggerAdmission(b *testing.B) {
	b.Setenv("SERVICE_ACCOUNT", "capsule")
	b.Setenv("NAMESPACE", "capsule-system")
	for _, count := range []int{0, 100, 1000} {
		for _, outcome := range []string{"match", "audience-skip", "managed-skip", "dry-run", "bookkeeping-skip"} {
			b.Run(fmt.Sprintf("unrelated=%d/%s", count, outcome), func(b *testing.B) {
				run, ctx, req, n, calls, _, _ := triggerFixture(b, count)
				switch outcome {
				case "audience-skip":
					req.UserInfo.Username = "bob"
				case "managed-skip":
					req.Object.Raw = []byte(`{"metadata":{"labels":{"projectcapsule.dev/managed-by":"controller"}}}`)
				case "dry-run":
					req.DryRun = new(true)
				case "bookkeeping-skip":
					req.Resource.Group = capsulev1beta2.GroupVersion.Group
					req.Resource.Resource = "tenantresources"
					req.UserInfo.Username = "system:serviceaccount:capsule-system:capsule"
				}
				b.ReportAllocs()
				for b.Loop() {
					if run(ctx, req) != nil {
						b.Fatal("terminated chain")
					}
				}
				if outcome == "match" && n.calls.Load() == 0 {
					b.Fatal("no notifications")
				}
				b.ReportMetric(float64(calls.Load())/float64(b.N), "cached-reads/op")
			})
		}
	}
}

func BenchmarkTriggerAdmissionObjectSize(b *testing.B) {
	for _, size := range []int{1024, 64 * 1024, 512 * 1024} {
		for _, parallel := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes=%d/parallel=%v", size, parallel), func(b *testing.B) {
				run, ctx, req, n, _, _, _ := triggerFixture(b, 0)
				obj := &corev1.Secret{Name: "source", Namespace: "team-a", Data: map[string][]byte{"value": make([]byte, size)}}
				var err error
				req.Object.Raw, err = json.Marshal(obj)
				if err != nil {
					b.Fatal(err)
				}
				req.OldObject.Raw = req.Object.Raw
				b.ReportAllocs()
				b.ResetTimer()
				if parallel {
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							run(ctx, req)
						}
					})
				} else {
					for range b.N {
						run(ctx, req)
					}
				}
				b.StopTimer()
				if n.calls.Load() != int64(b.N) {
					b.Fatal("matching object did not notify")
				}
			})
		}
	}
}
