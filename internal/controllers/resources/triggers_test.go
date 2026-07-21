// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/watch"
)

var (
	secretGVK    = schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
	configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
)

func testScheme(t testing.TB) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add capsule scheme: %v", err)
	}

	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 scheme: %v", err)
	}

	return scheme
}

func secretTrigger(ops ...capsulev1beta2.TriggerOperation) capsulev1beta2.TriggerSpec {
	return capsulev1beta2.TriggerSpec{
		VersionKinds: capruntime.VersionKinds{Kinds: []string{"Secret"}},
		Operations:   ops,
	}
}

// collectEnqueued returns an enqueue func that records keys so each Notify's
// output is observable synchronously.
func collectEnqueued() (func(types.NamespacedName), *[]types.NamespacedName) {
	var got []types.NamespacedName

	return func(key types.NamespacedName) { got = append(got, key) }, &got
}

// fakeResolver resolves Secret and ConfigMap as namespaced and Namespace as
// cluster-scoped, mirroring the real REST mapper for the test kinds.
type fakeResolver struct{}

func (fakeResolver) ResolveVersionKinds(vks []capruntime.VersionKind) (namespaced, clusterScoped []schema.GroupVersionKind, _ error) {
	for _, vk := range vks {
		switch vk.Kind {
		case "Namespace":
			clusterScoped = append(clusterScoped, schema.GroupVersionKind{Version: "v1", Kind: "Namespace"})
		default:
			namespaced = append(namespaced, schema.GroupVersionKind{Version: "v1", Kind: vk.Kind})
		}
	}

	return namespaced, clusterScoped, nil
}

func TestTriggerWatchSpecs(t *testing.T) {
	namespaceGVK := schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	selA := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}
	selB := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "b"}}

	triggers := []capsulev1beta2.TriggerSpec{
		// Trigger selectors are matched in the sinks, never pushed down: all
		// triggers of a kind share one watch.
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"Secret"}}, Selector: selA},
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"Secret"}}, Selector: selB},
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"ConfigMap"}}, Selector: selA},
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"ConfigMap"}}},
		// Cluster-scoped kind: watched for a cluster-scoped owner.
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"Namespace"}}},
	}

	specs, rejected, err := triggerWatchSpecs(fakeResolver{}, triggers, false)
	if err != nil || len(rejected) != 0 {
		t.Fatalf("unexpected error/rejection: %v %v", err, rejected)
	}

	if len(specs) != 3 {
		t.Fatalf("expected one spec per distinct kind (3), got %d", len(specs))
	}

	seen := map[schema.GroupVersionKind]bool{}

	for _, s := range specs {
		if seen[s.GVK] {
			t.Fatalf("kind %s must be watched exactly once", s.GVK)
		}

		seen[s.GVK] = true

		if s.Selector != nil {
			t.Fatalf("global watches must be unfiltered, got %v for %v", s.Selector, s.GVK)
		}
	}

	for _, g := range []schema.GroupVersionKind{secretGVK, configMapGVK, namespaceGVK} {
		if !seen[g] {
			t.Fatalf("missing watch for %v, got %v", g, seen)
		}
	}
}

func TestTriggerWatchSpecs_NamespacedOwner(t *testing.T) {
	userSel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}

	triggers := []capsulev1beta2.TriggerSpec{
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"Secret"}}, Selector: userSel},
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"ConfigMap"}}},
		// Cluster-scoped kind, referenced twice: rejected once.
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"Namespace"}}},
		{VersionKinds: capruntime.VersionKinds{Kinds: []string{"Namespace"}}, Selector: userSel},
	}

	specs, rejected, err := triggerWatchSpecs(fakeResolver{}, triggers, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rejected) != 1 || rejected[0] != "/v1, Kind=Namespace" {
		t.Fatalf("expected namespace rejected once, got %v", rejected)
	}

	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	// Every tenant-scoped watch carries exactly the tenant-label-exists
	// selector: the user selector stays sink-side, so all TenantResources
	// share one informer per kind across all tenants.
	for _, s := range specs {
		if s.Selector == nil {
			t.Fatalf("every tenant-scoped watch must carry a selector, got nil for %v", s.GVK)
		}

		if len(s.Selector.MatchLabels) != 0 || len(s.Selector.MatchExpressions) != 1 {
			t.Fatalf("watch must carry only the tenant-label requirement, got %v for %v", s.Selector, s.GVK)
		}

		req := s.Selector.MatchExpressions[0]
		if req.Key != capsulemeta.NewTenantLabel || req.Operator != metav1.LabelSelectorOpExists || len(req.Values) != 0 {
			t.Fatalf("watch must require the tenant label to exist, got %v for %v", req, s.GVK)
		}
	}
}

func TestGlobalTriggerSink_Matching(t *testing.T) {
	scheme := testScheme(t)

	gtr := &capsulev1beta2.GlobalTenantResource{
		ObjectMeta: metav1.ObjectMeta{Name: "g1"},
	}
	gtr.Spec.Triggers = []capsulev1beta2.TriggerSpec{
		func() capsulev1beta2.TriggerSpec {
			t := secretTrigger(capsulev1beta2.TriggerOperationCreate, capsulev1beta2.TriggerOperationUpdate)
			t.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}

			return t
		}(),
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gtr).
		WithIndex(&capsulev1beta2.GlobalTenantResource{}, tenantresource.TriggersIndexerFieldName, tenantresource.GlobalTriggers{}.Func()).
		Build()

	enqueue, got := collectEnqueued()
	sink := &globalTriggerSink{reader: cl, enqueue: enqueue, log: logr.Discard()}

	matching := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Name: "s1", Namespace: "ns", Labels: map[string]string{"app": "x"},
	}}

	// Matching label + allowed op enqueues.
	sink.Notify(context.Background(), secretGVK, watch.OperationCreate, matching)
	if len(*got) != 1 || (*got)[0].Name != "g1" {
		t.Fatalf("expected g1 enqueued, got %v", *got)
	}

	*got = nil

	// Disallowed operation is ignored.
	sink.Notify(context.Background(), secretGVK, watch.OperationDelete, matching)
	if len(*got) != 0 {
		t.Fatalf("DELETE must not enqueue, got %v", *got)
	}

	// Non-matching label is ignored.
	nonMatching := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s2", Namespace: "ns"}}
	sink.Notify(context.Background(), secretGVK, watch.OperationCreate, nonMatching)

	if len(*got) != 0 {
		t.Fatalf("non-matching label must not enqueue, got %v", *got)
	}

	// A capsule-replicated object enqueues too: triggers may watch the rendered
	// objects themselves (restore on delete, revert on tampering). The
	// self-trigger echo is not filtered here; it converges once the rendered
	// output stops changing.
	owned := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Name:      "owned",
		Namespace: "ns",
		Labels: map[string]string{
			"app":                             "x",
			capsulemeta.CreatedByCapsuleLabel: capsulemeta.ValueControllerReplications,
		},
	}}
	sink.Notify(context.Background(), secretGVK, watch.OperationCreate, owned)

	if len(*got) != 1 {
		t.Fatalf("capsule-replicated object must enqueue, got %v", *got)
	}
}

func TestGlobalTriggerSink_NamespaceSelector(t *testing.T) {
	scheme := testScheme(t)

	trig := secretTrigger()
	trig.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}}

	gtr := &capsulev1beta2.GlobalTenantResource{ObjectMeta: metav1.ObjectMeta{Name: "g1"}}
	gtr.Spec.Triggers = []capsulev1beta2.TriggerSpec{trig}

	prod := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-prod", Labels: map[string]string{"env": "prod"}}}
	dev := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-dev", Labels: map[string]string{"env": "dev"}}}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gtr, prod, dev).
		WithIndex(&capsulev1beta2.GlobalTenantResource{}, tenantresource.TriggersIndexerFieldName, tenantresource.GlobalTriggers{}.Func()).
		Build()

	enqueue, got := collectEnqueued()
	sink := &globalTriggerSink{reader: cl, enqueue: enqueue, log: logr.Discard()}

	inProd := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns-prod"}}
	sink.Notify(context.Background(), secretGVK, watch.OperationUpdate, inProd)

	if len(*got) != 1 {
		t.Fatalf("object in prod namespace must enqueue, got %v", *got)
	}

	*got = nil

	inDev := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns-dev"}}
	sink.Notify(context.Background(), secretGVK, watch.OperationUpdate, inDev)

	if len(*got) != 0 {
		t.Fatalf("object in dev namespace must not enqueue, got %v", *got)
	}
}

func TestNamespacedTriggerSink_TenantScoping(t *testing.T) {
	scheme := testScheme(t)

	tr := &capsulev1beta2.TenantResource{ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "tenant-a-ns1"}}
	tr.Spec.Triggers = []capsulev1beta2.TriggerSpec{{
		VersionKinds: capruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"}},
	}}

	nsA1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a-ns1", Labels: map[string]string{capsulemeta.TenantLabel: "a"}}}
	nsA2 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a-ns2", Labels: map[string]string{capsulemeta.TenantLabel: "a"}}}
	nsB1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-b-ns1", Labels: map[string]string{capsulemeta.TenantLabel: "b"}}}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(tr, nsA1, nsA2, nsB1).
		WithIndex(&capsulev1beta2.TenantResource{}, tenantresource.TriggersIndexerFieldName, tenantresource.NamespacedTriggers{}.Func()).
		Build()

	enqueue, got := collectEnqueued()
	sink := &namespacedTriggerSink{reader: cl, enqueue: enqueue, log: logr.Discard()}

	// Object in another namespace of the SAME tenant fires the TenantResource.
	sameTenant := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "tenant-a-ns2"}}
	sink.Notify(context.Background(), configMapGVK, watch.OperationUpdate, sameTenant)

	if len(*got) != 1 || (*got)[0].Name != "t1" || (*got)[0].Namespace != "tenant-a-ns1" {
		t.Fatalf("same-tenant change must enqueue t1, got %v", *got)
	}

	*got = nil

	// Object in another tenant's namespace must NOT fire.
	otherTenant := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "tenant-b-ns1"}}
	sink.Notify(context.Background(), configMapGVK, watch.OperationUpdate, otherTenant)

	if len(*got) != 0 {
		t.Fatalf("cross-tenant change must not enqueue, got %v", *got)
	}

	// Cluster-scoped object (no namespace) must NOT fire.
	clusterScoped := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "cm"}}
	sink.Notify(context.Background(), configMapGVK, watch.OperationUpdate, clusterScoped)

	if len(*got) != 0 {
		t.Fatalf("cluster-scoped change must not enqueue, got %v", *got)
	}

	// A capsule-replicated object enqueues too: triggers may watch the rendered
	// objects themselves. The self-trigger echo converges once the rendered
	// output stops changing.
	owned := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
		Name:      "cm",
		Namespace: "tenant-a-ns2",
		Labels:    map[string]string{capsulemeta.CreatedByCapsuleLabel: capsulemeta.ValueControllerReplications},
	}}
	sink.Notify(context.Background(), configMapGVK, watch.OperationUpdate, owned)

	if len(*got) != 1 {
		t.Fatalf("capsule-replicated object must enqueue, got %v", *got)
	}
}

func TestNewTriggersCondition(t *testing.T) {
	obj := &capsulev1beta2.GlobalTenantResource{}

	if c := newTriggersCondition(obj, 0, nil); c.Status != metav1.ConditionTrue || c.Message != "no triggers configured" {
		t.Fatalf("unexpected empty condition: %+v", c)
	}

	if c := newTriggersCondition(obj, 3, nil); c.Status != metav1.ConditionTrue || c.Message != "watching 3 kind(s)" {
		t.Fatalf("unexpected active condition: %+v", c)
	}

	if c := newTriggersCondition(obj, 0, context.DeadlineExceeded); c.Status != metav1.ConditionFalse || c.Reason != capsulemeta.FailedReason {
		t.Fatalf("unexpected error condition: %+v", c)
	}
}
