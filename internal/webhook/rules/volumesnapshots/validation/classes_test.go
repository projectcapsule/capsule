// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	webhookutils "github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type countingClient struct {
	client.Client
	gets, classGets atomic.Int64
	classErr        error
}

func (c *countingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)

	if _, ok := obj.(*snapshotv1.VolumeSnapshotClass); ok {
		c.classGets.Add(1)

		if c.classErr != nil {
			return c.classErr
		}
	}

	return c.Client.Get(ctx, key, obj, opts...)
}

type recordingEvents struct {
	events.EventRecorder
	emitted []events.LabeledEvent
}

func (r *recordingEvents) LabeledEvent(obj runtime.Object, eventType, reason, action, note string) events.LabeledEvent {
	return &recordedEvent{LabeledEvent: r.EventRecorder.LabeledEvent(obj, eventType, reason, action, note), recorder: r}
}

type recordedEvent struct {
	events.LabeledEvent
	recorder *recordingEvents
}

func (e *recordedEvent) Emit(context.Context) {
	e.recorder.emitted = append(e.recorder.emitted, e.LabeledEvent)
}

func (e *recordedEvent) WithRelated(obj runtime.Object) events.LabeledEvent {
	e.LabeledEvent.WithRelated(obj)
	return e
}

func (e *recordedEvent) WithTenantLabel(tnt *capsulev1beta2.Tenant) events.LabeledEvent {
	e.LabeledEvent.WithTenantLabel(tnt)
	return e
}

func (e *recordedEvent) WithRequestAnnotations(req admission.Request) events.LabeledEvent {
	e.LabeledEvent.WithRequestAnnotations(req)
	return e
}

func (r *recordingEvents) reasons() []string {
	reasons := make([]string, 0, len(r.emitted))
	for _, event := range r.emitted {
		reasons = append(reasons, event.Reason())
	}
	return reasons
}

// profile is the effective rule set of one tenant namespace. Without status,
// the handler renders the tenant rules through the namespace selector fallback.
type profile struct {
	tenant, namespace string
	labels            map[string]string
	rules             []*rules.NamespaceRuleBodyNamespace
	tenantRules       []*rules.NamespaceRuleBodyTenant
	status            bool
}

func testScheme(tb testing.TB) *runtime.Scheme {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, corev1.AddToScheme(scheme))
	require.NoError(tb, capsulev1beta2.AddToScheme(scheme))
	require.NoError(tb, snapshotv1.AddToScheme(scheme))
	return scheme
}

func fixture(tb testing.TB, profiles ...profile) (*countingClient, admission.Decoder) {
	tb.Helper()
	scheme := testScheme(tb)
	objects := []client.Object{
		&snapshotv1.VolumeSnapshotClass{Name: "gold", Labels: map[string]string{"env": "customer"}, Driver: "csi.example.com"},
		&snapshotv1.VolumeSnapshotClass{Name: "silver", Driver: "csi.example.com"},
		&snapshotv1.VolumeSnapshotClass{Name: "bronze", Labels: map[string]string{"env": "internal"}, Driver: "csi.example.com"},
	}
	tenants := map[string]*capsulev1beta2.Tenant{}
	for _, p := range profiles {
		tnt, found := tenants[p.tenant]
		if !found {
			tnt = &capsulev1beta2.Tenant{Name: p.tenant, UID: types.UID(p.tenant)}
			tenants[p.tenant] = tnt
			objects = append(objects, tnt)
		}
		tnt.Spec.Rules = append(tnt.Spec.Rules, p.tenantRules...)
		ns := &corev1.Namespace{Name: p.namespace, Labels: p.labels, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: p.tenant, UID: tnt.UID}}}
		objects = append(objects, ns)
		if p.status {
			rs := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: p.namespace}
			rs.Status.Rules = p.rules
			objects = append(objects, rs)
		}
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&capsulev1beta2.RuleStatus{}).Build()
	return &countingClient{Client: cl}, admission.NewDecoder(scheme)
}

func singleProfile(tb testing.TB, bodies ...*rules.NamespaceRuleBodyNamespace) (*countingClient, admission.Decoder) {
	return fixture(tb, profile{tenant: "tenant-a", namespace: "tenant-a", rules: bodies, status: true})
}

func classRule(action rules.ActionType, targets []rules.StorageValidationTarget, classes ...rules.StorageClassMatch) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action:  action,
		Storage: rules.NamespaceRuleEnforceStorageBody{Targets: targets, Classes: classes},
	}}
}

var (
	snapshotTargets = []rules.StorageValidationTarget{rules.StorageTargetVolumeSnapshot}
	customerClasses = rules.StorageClassMatch{MatchLabels: map[string]string{"env": "customer"}}
)

func allowList() *rules.NamespaceRuleBodyNamespace {
	return classRule(rules.ActionTypeAllow, snapshotTargets,
		rules.StorageClassMatch{Exact: []string{"silver"}}, rules.StorageClassMatch{Expression: "^csi-"}, customerClasses)
}

func allowNames(names ...string) *rules.NamespaceRuleBodyNamespace {
	return classRule(rules.ActionTypeAllow, snapshotTargets, rules.StorageClassMatch{Exact: names})
}

func denyNames(names ...string) *rules.NamespaceRuleBodyNamespace {
	return classRule(rules.ActionTypeDeny, nil, rules.StorageClassMatch{Exact: names})
}

// forAlice scopes a rule to the requesting user, so it would not check the
// snapshot-controller's later default-class patch.
func forAlice(body *rules.NamespaceRuleBodyNamespace) *rules.NamespaceRuleBodyNamespace {
	body.Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}
	return body
}

func groupOwner(apiVersion string) func(*snapshotv1.VolumeSnapshot) {
	return func(obj *snapshotv1.VolumeSnapshot) {
		obj.OwnerReferences = []metav1.OwnerReference{{APIVersion: apiVersion, Kind: "VolumeGroupSnapshot", Name: "group", UID: "group"}}
	}
}

func newSnapshot(namespace string, class *string, mutate ...func(*snapshotv1.VolumeSnapshot)) *snapshotv1.VolumeSnapshot {
	obj := &snapshotv1.VolumeSnapshot{
		APIVersion: snapshotv1.SchemeGroupVersion.String(), Kind: "VolumeSnapshot",
		Name: "data", Namespace: namespace, UID: "data",
		Spec: snapshotv1.VolumeSnapshotSpec{
			Source:                  snapshotv1.VolumeSnapshotSource{PersistentVolumeClaimName: new("data")},
			VolumeSnapshotClassName: class,
		},
	}
	for _, fn := range mutate {
		fn(obj)
	}
	return obj
}

func request(tb testing.TB, obj, old *snapshotv1.VolumeSnapshot) admission.Request {
	tb.Helper()
	raw, err := json.Marshal(obj)
	require.NoError(tb, err)
	req := admission.Request{
		Kind:      metav1.GroupVersionKind{Group: snapshotv1.GroupName, Version: "v1", Kind: "VolumeSnapshot"},
		Resource:  metav1.GroupVersionResource{Group: snapshotv1.GroupName, Version: "v1", Resource: "volumesnapshots"},
		Namespace: obj.Namespace, Name: obj.Name, Operation: admissionv1.Create,
		Object: runtime.RawExtension{Raw: raw}, UserInfo: authenticationv1.UserInfo{Username: "alice"}}
	if old != nil {
		oldRaw, err := json.Marshal(old)
		require.NoError(tb, err)
		req.Operation, req.OldObject = admissionv1.Update, runtime.RawExtension{Raw: oldRaw}
	}
	return req
}

func admit(ctx context.Context, cl *countingClient, decoder admission.Decoder, recorder events.EventRecorder, req admission.Request) *admission.Response {
	handler := Handler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), compiler)
	return run(ctx, handler, cl, decoder, recorder, req)
}

func run(ctx context.Context, handler handlers.Handler, cl *countingClient, decoder admission.Decoder, recorder events.EventRecorder, req admission.Request) *admission.Response {
	reader := webhookutils.NewRequestCachingReader(cl)
	if req.Operation == admissionv1.Update {
		return handler.OnUpdate(cl, reader, decoder, recorder)(ctx, req)
	}
	return handler.OnCreate(cl, reader, decoder, recorder)(ctx, req)
}

var compiler = func() *cache.CELCache {
	c, err := cache.NewCELCache()
	if err != nil {
		panic(err)
	}
	return c
}()

func newRecorder() *recordingEvents {
	return &recordingEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
}

const (
	notAllowed = `volume snapshot class "bronze" at spec.volumeSnapshotClassName is not allowed by namespace rule`
	required   = "volume snapshot class is required at spec.volumeSnapshotClassName because class rules target volumesnapshot"
)

func TestVolumeSnapshotClassRules(t *testing.T) {
	preProvisioned := func(obj *snapshotv1.VolumeSnapshot) {
		obj.Spec.Source = snapshotv1.VolumeSnapshotSource{VolumeSnapshotContentName: new("restored-content")}
	}
	nightly := func(obj *snapshotv1.VolumeSnapshot) { obj.Labels = map[string]string{"backup": "nightly"} }
	conditional := func() *rules.NamespaceRuleBodyNamespace {
		body := allowNames("silver")
		body.Enforce.Conditions = []rules.AdmissionCondition{{Name: "nightly", Expression: `has(object.metadata.labels) && "backup" in object.metadata.labels && object.metadata.labels["backup"] == "nightly"`}}
		return body
	}

	for _, tc := range []struct {
		name       string
		rules      []*rules.NamespaceRuleBodyNamespace
		class      *string
		mutate     []func(*snapshotv1.VolumeSnapshot)
		denied     string
		reasons    []string
		classReads int64
	}{
		{name: "exact name is allowed", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, class: new("silver"), classReads: 1},
		{name: "expression is allowed without a class object", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, class: new("csi-hostpath"), classReads: 1},
		{name: "class labels are allowed", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, class: new("gold"), classReads: 1},
		{
			name: "allow list miss is denied", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, class: new("bronze"), classReads: 1,
			denied:  notAllowed + ". Allowed classes: exact: silver, exp: ^csi-, selector: env=customer",
			reasons: []string{events.ReasonForbiddenVolumeSnapshotClass},
		},
		{
			name: "missing class object never matches labels", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, class: new("ghost"), classReads: 1,
			denied: `volume snapshot class "ghost" at spec.volumeSnapshotClassName is not allowed by namespace rule`, reasons: []string{events.ReasonForbiddenVolumeSnapshotClass},
		},
		{name: "name matches never read the class", rules: []*rules.NamespaceRuleBodyNamespace{allowNames("silver")}, class: new("silver")},
		{
			name: "deny match", rules: []*rules.NamespaceRuleBodyNamespace{denyNames("bronze")}, class: new("bronze"),
			denied: `volume snapshot class "bronze" at spec.volumeSnapshotClassName is denied by namespace rule`, reasons: []string{events.ReasonForbiddenVolumeSnapshotClass},
		},
		{name: "deny miss", rules: []*rules.NamespaceRuleBodyNamespace{denyNames("bronze")}, class: new("gold")},
		{name: "later allow overrides deny", rules: []*rules.NamespaceRuleBodyNamespace{denyNames("bronze"), allowNames("bronze")}, class: new("bronze")},
		{
			name: "later deny overrides allow", rules: []*rules.NamespaceRuleBodyNamespace{allowNames("bronze"), denyNames("bronze")}, class: new("bronze"),
			denied: "is denied by namespace rule", reasons: []string{events.ReasonForbiddenVolumeSnapshotClass},
		},
		{
			name: "audit records without blocking", rules: []*rules.NamespaceRuleBodyNamespace{classRule(rules.ActionTypeAudit, nil, rules.StorageClassMatch{Expression: ".*"})}, class: new("bronze"),
			reasons: []string{events.ReasonNamespaceRuleAudit},
		},
		{name: "allow requires a class", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, denied: required, reasons: []string{events.ReasonMissingVolumeSnapshotClass}},
		{name: "deny requires a class", rules: []*rules.NamespaceRuleBodyNamespace{denyNames("bronze")}, denied: required, reasons: []string{events.ReasonMissingVolumeSnapshotClass}},
		{
			name: "audit records a missing class", rules: []*rules.NamespaceRuleBodyNamespace{classRule(rules.ActionTypeAudit, nil, rules.StorageClassMatch{Expression: ".*"})},
			reasons: []string{events.ReasonNamespaceRuleAudit},
		},
		{name: "pre-provisioned snapshot may omit the class", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, mutate: []func(*snapshotv1.VolumeSnapshot){preProvisioned}},
		{
			name: "pre-provisioned snapshot class is still matched", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, class: new("bronze"), classReads: 1,
			mutate: []func(*snapshotv1.VolumeSnapshot){preProvisioned}, denied: notAllowed, reasons: []string{events.ReasonForbiddenVolumeSnapshotClass},
		},
		{name: "group snapshot member may omit the class", rules: []*rules.NamespaceRuleBodyNamespace{allowList()}, mutate: []func(*snapshotv1.VolumeSnapshot){groupOwner("groupsnapshot.storage.k8s.io/v1")}},
		{
			name: "owner from another group is not a group member", rules: []*rules.NamespaceRuleBodyNamespace{allowList()},
			mutate: []func(*snapshotv1.VolumeSnapshot){groupOwner("example.com/v1")}, denied: required, reasons: []string{events.ReasonMissingVolumeSnapshotClass},
		},
		// The snapshot-controller only treats owners with its own API version as
		// group members, and defaults the class of any other snapshot.
		{
			name: "group owner with an unrecognized version is not a group member", rules: []*rules.NamespaceRuleBodyNamespace{forAlice(allowList())},
			mutate: []func(*snapshotv1.VolumeSnapshot){groupOwner("groupsnapshot.storage.k8s.io/v999")}, denied: required, reasons: []string{events.ReasonMissingVolumeSnapshotClass},
		},
		{
			name: "group owner with an older version is not a group member", rules: []*rules.NamespaceRuleBodyNamespace{forAlice(allowList())},
			mutate: []func(*snapshotv1.VolumeSnapshot){groupOwner("groupsnapshot.storage.k8s.io/v1beta2")}, denied: required, reasons: []string{events.ReasonMissingVolumeSnapshotClass},
		},
		{
			name: "volume rules do not require a class",
			rules: []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Storage: rules.NamespaceRuleEnforceStorageBody{
				Volumes: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{}}},
			}}}},
		},
		{name: "other targets are ignored", rules: []*rules.NamespaceRuleBodyNamespace{classRule(rules.ActionTypeAllow, []rules.StorageValidationTarget{"persistentvolumeclaim/storageclass"}, rules.StorageClassMatch{Exact: []string{"silver"}})}, class: new("bronze")},
		{name: "unmatched condition skips the rule", rules: []*rules.NamespaceRuleBodyNamespace{conditional()}, class: new("bronze")},
		{
			name: "matched condition applies the rule", rules: []*rules.NamespaceRuleBodyNamespace{conditional()}, class: new("bronze"), mutate: []func(*snapshotv1.VolumeSnapshot){nightly},
			denied: notAllowed, reasons: []string{events.ReasonForbiddenVolumeSnapshotClass},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, decoder := singleProfile(t, tc.rules...)
			recorder := newRecorder()
			response := admit(t.Context(), cl, decoder, recorder, request(t, newSnapshot("tenant-a", tc.class, tc.mutate...), nil))
			if tc.denied == "" {
				require.Nil(t, response, "successful checks must continue the admission chain")
			} else {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, tc.denied)
			}
			if len(tc.reasons) == 0 {
				require.Empty(t, recorder.emitted)
			} else {
				require.Equal(t, tc.reasons, recorder.reasons())
				require.Equal(t, "tenant-a", recorder.emitted[0].Labels()[meta.NewTenantLabel])
			}
			require.Equal(t, tc.classReads, cl.classGets.Load())
		})
	}
}

func TestVolumeSnapshotClassUpdates(t *testing.T) {
	for _, tc := range []struct {
		name       string
		old, class *string
		denied     string
		skipped    bool
	}{
		{name: "label update keeps a class the rules no longer allow", old: new("bronze"), class: new("bronze"), skipped: true},
		{name: "finalizer update keeps a missing class", skipped: true},
		{name: "changing to a forbidden class", old: new("silver"), class: new("bronze"), denied: notAllowed},
		{name: "changing to an allowed class", old: new("bronze"), class: new("silver")},
		{name: "default class patch to a forbidden class", class: new("bronze"), denied: notAllowed},
		{name: "default class patch to an allowed class", class: new("gold")},
		{name: "removing the class", old: new("silver"), denied: required},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, decoder := singleProfile(t, allowList())
			old := newSnapshot("tenant-a", tc.old)
			obj := newSnapshot("tenant-a", tc.class, func(obj *snapshotv1.VolumeSnapshot) {
				obj.Labels = map[string]string{"retention": "weekly"}
				obj.Finalizers = []string{"snapshot.storage.kubernetes.io/volumesnapshot-as-source-protection"}
			})
			response := admit(t.Context(), cl, decoder, newRecorder(), request(t, obj, old))
			if tc.denied == "" {
				require.Nil(t, response)
			} else {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, tc.denied)
			}
			if tc.skipped {
				require.Zero(t, cl.gets.Load(), "an update that keeps the class must not read the tenant, rules or class")
			}
		})
	}

	t.Run("status updates are not validated", func(t *testing.T) {
		cl, decoder := singleProfile(t, allowList())
		req := request(t, newSnapshot("tenant-a", new("bronze")), newSnapshot("tenant-a", new("silver")))
		req.SubResource = "status"
		require.Nil(t, admit(t.Context(), cl, decoder, newRecorder(), req))
		require.Zero(t, cl.gets.Load())
	})

	// Without its group owner, an unbound snapshot without a class gets a
	// default class from the snapshot-controller, which a user-scoped rule
	// never checks.
	t.Run("removing the group owner of an unbound snapshot without a class", func(t *testing.T) {
		cl, decoder := singleProfile(t, forAlice(allowList()))
		member := newSnapshot("tenant-a", nil, groupOwner("groupsnapshot.storage.k8s.io/v1"))
		response := admit(t.Context(), cl, decoder, newRecorder(), request(t, newSnapshot("tenant-a", nil), member))
		require.NotNil(t, response)
		require.False(t, response.Allowed)
		require.Contains(t, response.Result.Message, required)
	})

	// The garbage collector removes the owner when a VolumeGroupSnapshot is
	// deleted with orphan propagation. A bound snapshot is never provisioned
	// again, so the update must not be blocked.
	t.Run("orphaning a bound group member is skipped", func(t *testing.T) {
		cl, decoder := singleProfile(t, allowList())
		bound := func(obj *snapshotv1.VolumeSnapshot) {
			obj.Status = &snapshotv1.VolumeSnapshotStatus{BoundVolumeSnapshotContentName: new("group-content")}
		}
		member := newSnapshot("tenant-a", nil, groupOwner("groupsnapshot.storage.k8s.io/v1"), bound)
		require.Nil(t, admit(t.Context(), cl, decoder, newRecorder(), request(t, newSnapshot("tenant-a", nil, bound), member)))
		require.Zero(t, cl.gets.Load())
	})
}

func TestVolumeSnapshotClassNamespaceProfilesAndTenants(t *testing.T) {
	selected := &metav1.LabelSelector{MatchLabels: map[string]string{"storage-profile": "restricted"}}
	tenantRule := &rules.NamespaceRuleBodyTenant{NamespaceSelector: selected, NamespaceRuleBodyNamespace: allowNames("silver")}
	cl, decoder := fixture(t,
		profile{tenant: "tenant-a", namespace: "a-restricted", rules: []*rules.NamespaceRuleBodyNamespace{allowNames("silver")}, status: true},
		profile{tenant: "tenant-a", namespace: "a-open", status: true},
		profile{tenant: "tenant-b", namespace: "b-restricted", rules: []*rules.NamespaceRuleBodyNamespace{allowNames("bronze")}, status: true},
		profile{tenant: "tenant-c", namespace: "c-restricted", labels: selected.MatchLabels, tenantRules: []*rules.NamespaceRuleBodyTenant{tenantRule}},
		profile{tenant: "tenant-c", namespace: "c-open"},
	)

	for _, tc := range []struct {
		namespace, class string
		denied           bool
	}{
		{namespace: "a-restricted", class: "silver"},
		{namespace: "a-restricted", class: "bronze", denied: true},
		{namespace: "a-open", class: "bronze"},
		{namespace: "b-restricted", class: "bronze"},
		{namespace: "b-restricted", class: "silver", denied: true},
		{namespace: "c-restricted", class: "silver"},
		{namespace: "c-restricted", class: "bronze", denied: true},
		{namespace: "c-open", class: "bronze"},
	} {
		t.Run(tc.namespace+"/"+tc.class, func(t *testing.T) {
			response := admit(t.Context(), cl, decoder, newRecorder(), request(t, newSnapshot(tc.namespace, new(tc.class)), nil))
			if !tc.denied {
				require.Nil(t, response)
				return
			}
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			require.Contains(t, response.Result.Message, `volume snapshot class "`+tc.class+`" at spec.volumeSnapshotClassName is not allowed by namespace rule`)
		})
	}

	t.Run("namespace outside any tenant", func(t *testing.T) {
		require.NoError(t, cl.Create(t.Context(), &corev1.Namespace{Name: "unmanaged"}))
		require.Nil(t, admit(t.Context(), cl, decoder, newRecorder(), request(t, newSnapshot("unmanaged", nil), nil)))
	})
}

func TestVolumeSnapshotClassReadErrors(t *testing.T) {
	cl, decoder := singleProfile(t, allowList())
	cl.classErr = errors.New("apiserver unavailable")
	response := admit(t.Context(), cl, decoder, newRecorder(), request(t, newSnapshot("tenant-a", new("gold")), nil))
	require.NotNil(t, response)
	require.False(t, response.Allowed)
	require.Equal(t, int32(http.StatusInternalServerError), response.Result.Code)
	require.Contains(t, response.Result.Message, `get VolumeSnapshotClass "gold": apiserver unavailable`)
}

func TestVolumeSnapshotClassDryRunEvents(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		cl, decoder := singleProfile(t, allowList())
		recorder := newRecorder()
		req := request(t, newSnapshot("tenant-a", new("bronze")), nil)
		req.DryRun = new(dryRun)
		response := admit(t.Context(), cl, decoder, recorder, req)
		require.NotNil(t, response)
		require.False(t, response.Allowed)
		if dryRun {
			require.Empty(t, recorder.emitted)
		} else {
			require.Equal(t, []string{events.ReasonForbiddenVolumeSnapshotClass}, recorder.reasons())
		}
	}
}
