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
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	webhookutils "github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type classClient struct {
	client.Client
	gets, classGets atomic.Int64
	classErr        error
}

func (c *classClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)

	if _, ok := obj.(*storagev1.VolumeAttributesClass); ok {
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

func (e *recordedEvent) WithTenantLabel(tnt *capsule.Tenant) events.LabeledEvent {
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

func newRecorder() *recordingEvents {
	return &recordingEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
}

// classProfile is the effective rule set of one tenant namespace. Without
// status, the handler renders the tenant rules through the namespace selector
// fallback.
type classProfile struct {
	tenant, namespace string
	labels            map[string]string
	rules             []*rules.NamespaceRuleBodyNamespace
	tenantRules       []*rules.NamespaceRuleBodyTenant
	status            bool
}

func classFixture(tb testing.TB, profiles ...classProfile) (*classClient, admission.Decoder) {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, corev1.AddToScheme(scheme))
	require.NoError(tb, storagev1.AddToScheme(scheme))
	require.NoError(tb, capsule.AddToScheme(scheme))
	parameters := map[string]string{"iops": "3000"}
	objects := []client.Object{
		&storagev1.VolumeAttributesClass{Name: "gold", Labels: map[string]string{"env": "customer"}, DriverName: "csi.example.com", Parameters: parameters},
		&storagev1.VolumeAttributesClass{Name: "silver", DriverName: "csi.example.com", Parameters: parameters},
		&storagev1.VolumeAttributesClass{Name: "bronze", Labels: map[string]string{"env": "internal"}, DriverName: "csi.example.com", Parameters: parameters},
	}
	tenants := map[string]*capsule.Tenant{}
	for _, p := range profiles {
		tnt, found := tenants[p.tenant]
		if !found {
			tnt = &capsule.Tenant{Name: p.tenant, UID: types.UID(p.tenant)}
			tenants[p.tenant] = tnt
			objects = append(objects, tnt)
		}
		tnt.Spec.Rules = append(tnt.Spec.Rules, p.tenantRules...)
		objects = append(objects, &corev1.Namespace{Name: p.namespace, Labels: p.labels, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsule.GroupVersion.String(), Kind: "Tenant", Name: p.tenant, UID: tnt.UID}}})
		if p.status {
			rs := &capsule.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: p.namespace}
			rs.Status.Rules = p.rules
			objects = append(objects, rs)
		}
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&capsule.RuleStatus{}).Build()
	return &classClient{Client: cl}, admission.NewDecoder(scheme)
}

func singleClassProfile(tb testing.TB, bodies ...*rules.NamespaceRuleBodyNamespace) (*classClient, admission.Decoder) {
	return classFixture(tb, classProfile{tenant: "tenant-a", namespace: "tenant-a", rules: bodies, status: true})
}

func attributeClassRule(action rules.ActionType, classes ...rules.StorageClassMatch) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action:  action,
		Storage: rules.NamespaceRuleEnforceStorageBody{AttributeClasses: classes},
	}}
}

var customerClasses = rules.StorageClassMatch{MatchLabels: map[string]string{"env": "customer"}}

func allowClasses() *rules.NamespaceRuleBodyNamespace {
	return attributeClassRule(rules.ActionTypeAllow,
		rules.StorageClassMatch{Exact: []string{"silver"}}, rules.StorageClassMatch{Expression: "^csi-"}, customerClasses)
}

func allowClassNames(names ...string) *rules.NamespaceRuleBodyNamespace {
	return attributeClassRule(rules.ActionTypeAllow, rules.StorageClassMatch{Exact: names})
}

func denyClassNames(names ...string) *rules.NamespaceRuleBodyNamespace {
	return attributeClassRule(rules.ActionTypeDeny, rules.StorageClassMatch{Exact: names})
}

func newClaim(namespace string, class *string, mutate ...func(*corev1.PersistentVolumeClaim)) *corev1.PersistentVolumeClaim {
	obj := &corev1.PersistentVolumeClaim{
		APIVersion: "v1", Kind: "PersistentVolumeClaim",
		Name: "data", Namespace: namespace, UID: "data",
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:               []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:                 corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
			StorageClassName:          new("csi"),
			VolumeAttributesClassName: class,
		},
	}
	for _, fn := range mutate {
		fn(obj)
	}
	return obj
}

// bound sets the state the PV controller and the external-resizer leave on a
// bound claim. Only bound claims can change their class.
func bound(current *string) func(*corev1.PersistentVolumeClaim) {
	return func(obj *corev1.PersistentVolumeClaim) {
		obj.Spec.VolumeName = "pv-data"
		obj.Status = corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound, CurrentVolumeAttributesClassName: current}
	}
}

func claimRequest(tb testing.TB, obj, old *corev1.PersistentVolumeClaim) admission.Request {
	tb.Helper()
	raw, err := json.Marshal(obj)
	require.NoError(tb, err)
	req := admission.Request{
		Kind:      metav1.GroupVersionKind{Version: "v1", Kind: "PersistentVolumeClaim"},
		Resource:  metav1.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"},
		Namespace: obj.Namespace, Name: obj.Name, Operation: admissionv1.Create,
		Object: runtime.RawExtension{Raw: raw}, UserInfo: authenticationv1.UserInfo{Username: "alice"}}
	if old != nil {
		oldRaw, err := json.Marshal(old)
		require.NoError(tb, err)
		req.Operation, req.OldObject = admissionv1.Update, runtime.RawExtension{Raw: oldRaw}
	}
	return req
}

var classCompiler = func() *cache.CELCache {
	c, err := cache.NewCELCache()
	if err != nil {
		panic(err)
	}
	return c
}()

func admitClaim(ctx context.Context, cl *classClient, decoder admission.Decoder, recorder events.EventRecorder, req admission.Request) *admission.Response {
	handler := AttributeClassHandler(nil, cache.NewRegexCache(), cache.NewLabelSelectorCache(), classCompiler)
	return runClaim(ctx, handler, cl, decoder, recorder, req)
}

func runClaim(ctx context.Context, handler handlers.Handler, cl *classClient, decoder admission.Decoder, recorder events.EventRecorder, req admission.Request) *admission.Response {
	reader := webhookutils.NewRequestCachingReader(cl)
	if req.Operation == admissionv1.Update {
		return handler.OnUpdate(cl, reader, decoder, recorder)(ctx, req)
	}
	return handler.OnCreate(cl, reader, decoder, recorder)(ctx, req)
}

func requireDenied(t *testing.T, response *admission.Response, message string) {
	t.Helper()
	require.NotNil(t, response)
	require.False(t, response.Allowed)
	require.Contains(t, response.Result.Message, message)
}

const classNotAllowed = `volume attributes class "bronze" at spec.volumeAttributesClassName is not allowed by namespace rule`

func TestVolumeAttributesClassRules(t *testing.T) {
	fast := func(obj *corev1.PersistentVolumeClaim) { obj.Labels = map[string]string{"tier": "fast"} }
	conditional := func() *rules.NamespaceRuleBodyNamespace {
		body := allowClassNames("silver")
		body.Enforce.Conditions = []rules.AdmissionCondition{{Name: "fast", Expression: `has(object.metadata.labels) && "tier" in object.metadata.labels && object.metadata.labels["tier"] == "fast"`}}
		return body
	}
	forBob := func(body *rules.NamespaceRuleBodyNamespace) *rules.NamespaceRuleBodyNamespace {
		body.Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "bob"}}
		return body
	}

	for _, tc := range []struct {
		name       string
		rules      []*rules.NamespaceRuleBodyNamespace
		class      *string
		mutate     []func(*corev1.PersistentVolumeClaim)
		denied     string
		reasons    []string
		classReads int64
		skipped    bool
	}{
		{name: "exact name is allowed", rules: []*rules.NamespaceRuleBodyNamespace{allowClasses()}, class: new("silver")},
		{name: "expression is allowed without a class object", rules: []*rules.NamespaceRuleBodyNamespace{allowClasses()}, class: new("csi-fast")},
		{name: "class labels are allowed", rules: []*rules.NamespaceRuleBodyNamespace{allowClasses()}, class: new("gold"), classReads: 1},
		{
			name: "allow list miss is denied", rules: []*rules.NamespaceRuleBodyNamespace{allowClasses()}, class: new("bronze"), classReads: 1,
			denied:  classNotAllowed + ". Allowed classes: exact: silver, exp: ^csi-, selector: env=customer",
			reasons: []string{events.ReasonForbiddenVolumeAttributesClass},
		},
		{
			name: "missing class object never matches labels", rules: []*rules.NamespaceRuleBodyNamespace{allowClasses()}, class: new("ghost"), classReads: 1,
			denied: `volume attributes class "ghost" at spec.volumeAttributesClassName is not allowed by namespace rule`, reasons: []string{events.ReasonForbiddenVolumeAttributesClass},
		},
		{name: "name matches never read the class", rules: []*rules.NamespaceRuleBodyNamespace{allowClassNames("silver")}, class: new("silver")},
		{
			name: "deny match", rules: []*rules.NamespaceRuleBodyNamespace{denyClassNames("bronze")}, class: new("bronze"),
			denied: `volume attributes class "bronze" at spec.volumeAttributesClassName is denied by namespace rule`, reasons: []string{events.ReasonForbiddenVolumeAttributesClass},
		},
		{name: "deny miss", rules: []*rules.NamespaceRuleBodyNamespace{denyClassNames("bronze")}, class: new("gold")},
		{name: "later allow overrides deny", rules: []*rules.NamespaceRuleBodyNamespace{denyClassNames("bronze"), allowClassNames("bronze")}, class: new("bronze")},
		{
			name: "later deny overrides allow", rules: []*rules.NamespaceRuleBodyNamespace{allowClassNames("bronze"), denyClassNames("bronze")}, class: new("bronze"),
			denied: "is denied by namespace rule", reasons: []string{events.ReasonForbiddenVolumeAttributesClass},
		},
		{
			// A name match only decides its own rule; a later rule still reads the class.
			name: "later deny selector overrides an allow name match", rules: []*rules.NamespaceRuleBodyNamespace{allowClassNames("gold"), attributeClassRule(rules.ActionTypeDeny, customerClasses)}, class: new("gold"), classReads: 1,
			denied: "is denied by namespace rule", reasons: []string{events.ReasonForbiddenVolumeAttributesClass},
		},
		{
			name: "audit records without blocking", rules: []*rules.NamespaceRuleBodyNamespace{attributeClassRule(rules.ActionTypeAudit, rules.StorageClassMatch{Expression: ".*"})}, class: new("bronze"),
			reasons: []string{events.ReasonNamespaceRuleAudit},
		},
		{
			name: "audit records every entry that matches", rules: []*rules.NamespaceRuleBodyNamespace{attributeClassRule(rules.ActionTypeAudit, rules.StorageClassMatch{Exact: []string{"gold"}}, customerClasses)}, class: new("gold"), classReads: 1,
			reasons: []string{events.ReasonNamespaceRuleAudit, events.ReasonNamespaceRuleAudit},
		},
		// Kubernetes applies no class to a claim without one, and has no default
		// class to set later, so there is nothing to match.
		{name: "claim without a class is not checked", rules: []*rules.NamespaceRuleBodyNamespace{allowClasses()}, skipped: true},
		{name: "claim with an empty class is not checked", rules: []*rules.NamespaceRuleBodyNamespace{allowClasses()}, class: new(""), skipped: true},
		{
			name: "volume rules do not check classes",
			rules: []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Storage: rules.NamespaceRuleEnforceStorageBody{
				Volumes: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{}}},
			}}}},
			class: new("bronze"),
		},
		{
			name: "snapshot class rules do not check attribute classes",
			rules: []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Storage: rules.NamespaceRuleEnforceStorageBody{
				Classes: []rules.StorageClassMatch{{Exact: []string{"silver"}}},
			}}}},
			class: new("bronze"),
		},
		{name: "rule for another audience is skipped", rules: []*rules.NamespaceRuleBodyNamespace{forBob(allowClassNames("silver"))}, class: new("bronze")},
		{name: "unmatched condition skips the rule", rules: []*rules.NamespaceRuleBodyNamespace{conditional()}, class: new("bronze")},
		{
			name: "matched condition applies the rule", rules: []*rules.NamespaceRuleBodyNamespace{conditional()}, class: new("bronze"), mutate: []func(*corev1.PersistentVolumeClaim){fast},
			denied: classNotAllowed, reasons: []string{events.ReasonForbiddenVolumeAttributesClass},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, decoder := singleClassProfile(t, tc.rules...)
			recorder := newRecorder()
			response := admitClaim(t.Context(), cl, decoder, recorder, claimRequest(t, newClaim("tenant-a", tc.class, tc.mutate...), nil))
			if tc.denied == "" {
				require.Nil(t, response, "successful checks must continue the admission chain")
			} else {
				requireDenied(t, response, tc.denied)
			}
			if len(tc.reasons) == 0 {
				require.Empty(t, recorder.emitted)
			} else {
				require.Equal(t, tc.reasons, recorder.reasons())
				require.Equal(t, "tenant-a", recorder.emitted[0].Labels()[meta.NewTenantLabel])
			}
			require.Equal(t, tc.classReads, cl.classGets.Load())
			if tc.skipped {
				require.Zero(t, cl.gets.Load(), "a claim without a class must not read the tenant, rules or class")
			}
		})
	}
}

func TestVolumeAttributesClassUpdates(t *testing.T) {
	labeled := func(obj *corev1.PersistentVolumeClaim) { obj.Labels = map[string]string{"retention": "weekly"} }
	resized := func(obj *corev1.PersistentVolumeClaim) {
		obj.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("2Gi")
	}

	for _, tc := range []struct {
		name     string
		old, obj *corev1.PersistentVolumeClaim
		denied   bool
		skipped  bool
	}{
		{name: "label update keeps a class the rules no longer allow", old: newClaim("tenant-a", new("bronze"), bound(new("bronze"))), obj: newClaim("tenant-a", new("bronze"), bound(new("bronze")), labeled), skipped: true},
		{name: "resize keeps a class the rules no longer allow", old: newClaim("tenant-a", new("bronze"), bound(new("bronze"))), obj: newClaim("tenant-a", new("bronze"), bound(new("bronze")), resized), skipped: true},
		{name: "changing to a forbidden class", old: newClaim("tenant-a", new("silver"), bound(new("silver"))), obj: newClaim("tenant-a", new("bronze"), bound(new("silver"))), denied: true},
		{name: "changing to an allowed class", old: newClaim("tenant-a", new("bronze"), bound(new("bronze"))), obj: newClaim("tenant-a", new("silver"), bound(new("bronze")))},
		{name: "setting a forbidden class", old: newClaim("tenant-a", nil, bound(nil)), obj: newClaim("tenant-a", new("bronze"), bound(nil)), denied: true},
		{name: "setting an allowed class", old: newClaim("tenant-a", nil, bound(nil)), obj: newClaim("tenant-a", new("gold"), bound(nil))},
		{name: "removing a class that was never applied", old: newClaim("tenant-a", new("bronze"), bound(nil)), obj: newClaim("tenant-a", nil, bound(nil)), skipped: true},
		{name: "clearing a class that was never applied", old: newClaim("tenant-a", new("bronze"), bound(nil)), obj: newClaim("tenant-a", new(""), bound(nil)), skipped: true},
		// Restoring the class applied to the volume is how Kubernetes cancels a
		// modification, for example one the CSI driver rejected as infeasible.
		{name: "restoring the applied class the rules no longer allow", old: newClaim("tenant-a", new("silver"), bound(new("bronze"))), obj: newClaim("tenant-a", new("bronze"), bound(new("bronze"))), skipped: true},
		{name: "changing to a forbidden class other than the applied one", old: newClaim("tenant-a", new("silver"), bound(new("gold"))), obj: newClaim("tenant-a", new("bronze"), bound(new("gold"))), denied: true},
		{
			// Only the stored claim tells which class is applied.
			name: "the applied class comes from the stored claim", old: newClaim("tenant-a", new("silver"), bound(new("silver"))),
			obj: newClaim("tenant-a", new("bronze"), bound(new("bronze"))), denied: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, decoder := singleClassProfile(t, allowClasses())
			response := admitClaim(t.Context(), cl, decoder, newRecorder(), claimRequest(t, tc.obj, tc.old))
			if tc.denied {
				requireDenied(t, response, classNotAllowed)
			} else {
				require.Nil(t, response)
			}
			if tc.skipped {
				require.Zero(t, cl.gets.Load(), "an update that keeps or restores the class must not read the tenant, rules or class")
			}
		})
	}

	t.Run("status updates are not validated", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, allowClasses())
		req := claimRequest(t, newClaim("tenant-a", new("bronze"), bound(new("bronze"))), newClaim("tenant-a", new("silver"), bound(new("silver"))))
		req.SubResource = "status"
		require.Nil(t, admitClaim(t.Context(), cl, decoder, newRecorder(), req))
		require.Zero(t, cl.gets.Load())
	})

	t.Run("other kinds are not validated", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, allowClasses())
		req := claimRequest(t, newClaim("tenant-a", new("bronze")), nil)
		req.Kind = metav1.GroupVersionKind{Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshot"}
		require.Nil(t, admitClaim(t.Context(), cl, decoder, newRecorder(), req))
		require.Zero(t, cl.gets.Load())
	})
}

func TestVolumeAttributesClassNamespaceProfilesAndTenants(t *testing.T) {
	selected := &metav1.LabelSelector{MatchLabels: map[string]string{"storage-profile": "restricted"}}
	tenantRule := &rules.NamespaceRuleBodyTenant{NamespaceSelector: selected, NamespaceRuleBodyNamespace: allowClassNames("silver")}
	cl, decoder := classFixture(t,
		classProfile{tenant: "tenant-a", namespace: "a-restricted", rules: []*rules.NamespaceRuleBodyNamespace{allowClassNames("silver")}, status: true},
		classProfile{tenant: "tenant-a", namespace: "a-open", status: true},
		classProfile{tenant: "tenant-b", namespace: "b-restricted", rules: []*rules.NamespaceRuleBodyNamespace{allowClassNames("bronze")}, status: true},
		classProfile{tenant: "tenant-c", namespace: "c-restricted", labels: selected.MatchLabels, tenantRules: []*rules.NamespaceRuleBodyTenant{tenantRule}},
		classProfile{tenant: "tenant-c", namespace: "c-open"},
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
			response := admitClaim(t.Context(), cl, decoder, newRecorder(), claimRequest(t, newClaim(tc.namespace, new(tc.class)), nil))
			if !tc.denied {
				require.Nil(t, response)
				return
			}
			requireDenied(t, response, `volume attributes class "`+tc.class+`" at spec.volumeAttributesClassName is not allowed by namespace rule`)
		})
	}

	t.Run("namespace outside any tenant", func(t *testing.T) {
		require.NoError(t, cl.Create(t.Context(), &corev1.Namespace{Name: "unmanaged"}))
		require.Nil(t, admitClaim(t.Context(), cl, decoder, newRecorder(), claimRequest(t, newClaim("unmanaged", new("bronze")), nil)))
	})
}

func TestVolumeAttributesClassReadErrors(t *testing.T) {
	t.Run("a failed class read is a server error", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, allowClasses())
		cl.classErr = errors.New("apiserver unavailable")
		response := admitClaim(t.Context(), cl, decoder, newRecorder(), claimRequest(t, newClaim("tenant-a", new("gold")), nil))
		require.NotNil(t, response)
		require.False(t, response.Allowed)
		require.Equal(t, int32(http.StatusInternalServerError), response.Result.Code)
		require.Contains(t, response.Result.Message, `get VolumeAttributesClass "gold": apiserver unavailable`)
	})

	// An entry that matches by name already matches, so its selector needs no
	// class read, and a failing read must not reject the request.
	t.Run("a name match in an entry with a selector reads no class", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, attributeClassRule(rules.ActionTypeAllow,
			rules.StorageClassMatch{Exact: []string{"silver"}, LabelSelector: customerClasses.LabelSelector}))
		cl.classErr = errors.New("apiserver unavailable")
		require.Nil(t, admitClaim(t.Context(), cl, decoder, newRecorder(), claimRequest(t, newClaim("tenant-a", new("silver")), nil)))
		require.Zero(t, cl.classGets.Load())
	})

	// The entries of an allow or deny rule are ORed, so a name match decides the
	// rule, and the selector entries next to it must not read the class.
	t.Run("a name match decides an allow rule without reading the class for its other entries", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, allowClasses())
		cl.classErr = errors.New("apiserver unavailable")
		require.Nil(t, admitClaim(t.Context(), cl, decoder, newRecorder(), claimRequest(t, newClaim("tenant-a", new("silver")), nil)))
		require.Zero(t, cl.classGets.Load())
	})

	// Rule validation rejects invalid expressions, but one that reaches admission
	// must still fail the request rather than be skipped for a later name match.
	t.Run("an invalid expression before a name match is still reported", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, attributeClassRule(rules.ActionTypeAllow, rules.StorageClassMatch{Expression: "("}, rules.StorageClassMatch{Exact: []string{"silver"}}))
		requireDenied(t, admitClaim(t.Context(), cl, decoder, newRecorder(), claimRequest(t, newClaim("tenant-a", new("silver")), nil)), "invalid rule")
	})

	t.Run("a name match decides a deny rule without reading the class for its other entries", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, attributeClassRule(rules.ActionTypeDeny, customerClasses, rules.StorageClassMatch{Exact: []string{"bronze"}}))
		cl.classErr = errors.New("apiserver unavailable")
		response := admitClaim(t.Context(), cl, decoder, newRecorder(), claimRequest(t, newClaim("tenant-a", new("bronze")), nil))
		require.NotNil(t, response)
		require.False(t, response.Allowed)
		require.Equal(t, int32(http.StatusForbidden), response.Result.Code)
		require.Zero(t, cl.classGets.Load())
	})

	t.Run("an undecodable claim continues to the typed handler", func(t *testing.T) {
		cl, decoder := singleClassProfile(t, allowClasses())
		req := claimRequest(t, newClaim("tenant-a", new("silver")), nil)
		req.Object.Raw = []byte(`{"spec":{"volumeAttributesClassName":`)
		response := admitClaim(t.Context(), cl, decoder, newRecorder(), req)
		require.NotNil(t, response)
		require.False(t, response.Allowed)
	})
}

func TestVolumeAttributesClassDryRunEvents(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		cl, decoder := singleClassProfile(t, allowClasses())
		recorder := newRecorder()
		req := claimRequest(t, newClaim("tenant-a", new("bronze")), nil)
		req.DryRun = new(dryRun)
		requireDenied(t, admitClaim(t.Context(), cl, decoder, recorder, req), classNotAllowed)
		if dryRun {
			require.Empty(t, recorder.emitted)
		} else {
			require.Equal(t, []string{events.ReasonForbiddenVolumeAttributesClass}, recorder.reasons())
		}
	}
}
