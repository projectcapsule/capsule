// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/webhook/pvc"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type volumeFixture struct {
	tenant             *capsule.Tenant
	ns                 *corev1.Namespace
	pv                 *corev1.PersistentVolume
	old, claim         *corev1.PersistentVolumeClaim
	body               *rules.NamespaceRuleBodyNamespace
	status             *capsule.RuleStatus
	req                admission.Request
	omitStatus, omitPV bool
	failKind           string
}

func newVolumeFixture() *volumeFixture {
	f := &volumeFixture{}
	f.body = &rules.NamespaceRuleBodyNamespace{
		Audience: []rules.Audience{{Kind: rules.AudienceKindGroup, Name: "restorers"}},
		Enforce: &rules.NamespaceRuleEnforceBody{
			Action:     rules.ActionTypeAllow,
			Conditions: []rules.AdmissionCondition{{Expression: `volume != null && has(volume.spec.claimRef) && volume.spec.claimRef.namespace == 'staging' && volume.spec.claimRef.name.startsWith(object.metadata.namespace + '-')`}},
			Storage:    rules.NamespaceRuleEnforceStorageBody{Volumes: []rules.PersistentVolumeMatch{{Name: "restore", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "restore"}}}}},
		},
	}
	f.tenant = &capsule.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: "tenant-a-uid"}, Spec: capsule.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: f.body, NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "restore"}}}}}}
	f.ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "destination", Labels: map[string]string{"profile": "restore"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsule.GroupVersion.String(), Kind: "Tenant", Name: f.tenant.Name, UID: f.tenant.UID}}}}
	f.status = &capsule.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: f.ns.Name}, Status: capsule.RuleStatusStatus{Rules: []*rules.NamespaceRuleBodyNamespace{f.body}}}
	f.old = &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: f.ns.Name}, Spec: corev1.PersistentVolumeClaimSpec{Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: meta.TenantLabel, Operator: metav1.LabelSelectorOpIn, Values: []string{f.tenant.Name}}}}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}
	f.claim = f.old.DeepCopy()
	f.claim.Spec.VolumeName = "restored"
	f.pv = &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: f.claim.Spec.VolumeName, Labels: map[string]string{"pool": "restore", "keep": "original"}}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Name: "destination-copy", Namespace: "staging"}}}
	f.req = admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Update, Namespace: f.ns.Name, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "PersistentVolumeClaim"}, UserInfo: authenticationv1.UserInfo{Username: "system:serviceaccount:staging:restorer", Groups: []string{"restorers"}}}}
	return f
}

func (f *volumeFixture) client(t testing.TB) *volumeClient {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, capsule.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	objects := []client.Object{f.tenant, f.ns}
	if !f.omitPV {
		objects = append(objects, f.pv)
	}
	if !f.omitStatus {
		objects = append(objects, f.status)
	}
	return &volumeClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(), gets: map[string]int{}, failKind: f.failKind}
}

func (f *volumeFixture) request(t testing.TB) admission.Request {
	t.Helper()
	req := f.req
	var err error
	req.Object.Raw, err = json.Marshal(f.claim)
	if err != nil {
		t.Fatal(err)
	}
	req.OldObject.Raw, err = json.Marshal(f.old)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

type volumeClient struct {
	client.Client
	gets     map[string]int
	failKind string
	readPV   *corev1.PersistentVolume
}

func (c *volumeClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	kind := fmt.Sprintf("%T", obj)
	c.gets[kind]++
	if kind == c.failKind {
		return errors.New("injected read failure")
	}
	err := c.Client.Get(ctx, key, obj, opts...)
	if pv, ok := obj.(*corev1.PersistentVolume); ok && err == nil {
		c.readPV = pv
	}
	return err
}

func TestVolumeAccessAdmission(t *testing.T) {
	for _, tc := range []struct {
		name        string
		change      func(*volumeFixture)
		want        string
		statusReads int
	}{
		{name: "restore handoff", statusReads: 1},
		{name: "explicit binding create", change: func(f *volumeFixture) { f.req.Operation = admissionv1.Create }, statusReads: 1},
		{name: "same tenant unaffected by nonmatching rules", change: func(f *volumeFixture) {
			f.pv.Labels[meta.TenantLabel] = f.tenant.Name
			f.body.Enforce.Conditions[0].Expression = "false"
		}},
		{name: "other tenant cannot be overridden", change: func(f *volumeFixture) {
			f.pv.Labels[meta.TenantLabel] = "tenant-b"
			f.body.Enforce.Storage.Volumes[0].Selector = &metav1.LabelSelector{}
		}, want: "cross-tenant mount"},
		{name: "empty ownership label cannot be overridden", change: func(f *volumeFixture) { f.pv.Labels[meta.TenantLabel] = "" }, want: "cross-tenant mount"},
		{name: "nil PV labels wildcard", change: func(f *volumeFixture) {
			f.pv.Labels = nil
			f.body.Enforce.Storage.Volumes[0].Selector = &metav1.LabelSelector{}
		}, statusReads: 1},
		{name: "PVC labels cannot satisfy PV selector", change: func(f *volumeFixture) {
			delete(f.pv.Labels, "pool")
			f.claim.Labels = map[string]string{"pool": "restore"}
		}, want: "missing the Tenant label", statusReads: 1},
		{name: "read-only match expressions", change: func(f *volumeFixture) {
			f.body.Enforce.Storage.Volumes[0].Selector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "pool", Operator: metav1.LabelSelectorOpIn, Values: []string{"restore"}}}}
		}, statusReads: 1},
		{name: "wrong audience", change: func(f *volumeFixture) { f.req.UserInfo.Groups = nil }, want: "missing the Tenant label", statusReads: 1},
		{name: "false condition", change: func(f *volumeFixture) { f.pv.Spec.ClaimRef.Name = "another-namespace-copy" }, want: "missing the Tenant label", statusReads: 1},
		{name: "missing claim ref", change: func(f *volumeFixture) { f.pv.Spec.ClaimRef = nil }, want: "missing the Tenant label", statusReads: 1},
		{name: "condition error", change: func(f *volumeFixture) { f.body.Enforce.Conditions[0].Expression = "volume.spec.unknown == 'x'" }, want: "volume access conditions", statusReads: 1},
		{name: "audit cannot grant", change: func(f *volumeFixture) { f.body.Enforce.Action = rules.ActionTypeAudit }, want: "missing the Tenant label", statusReads: 1},
		{name: "deny cannot grant", change: func(f *volumeFixture) { f.body.Enforce.Action = rules.ActionTypeDeny }, want: "missing the Tenant label", statusReads: 1},
		{name: "later deny removes exception", change: func(f *volumeFixture) {
			deny := f.body.DeepCopy()
			deny.Enforce.Action = rules.ActionTypeDeny
			f.status.Status.Rules = append(f.status.Status.Rules, deny)
		}, want: "missing the Tenant label", statusReads: 1},
		{name: "later allow restores exception", change: func(f *volumeFixture) {
			deny := f.body.DeepCopy()
			deny.Enforce.Action = rules.ActionTypeDeny
			f.status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{deny, f.body}
		}, statusReads: 1},
		{name: "empty namespace profile", change: func(f *volumeFixture) { f.status.Status.Rules = nil }, want: "missing the Tenant label", statusReads: 1},
		{name: "no feature no rules lookup", change: func(f *volumeFixture) { f.tenant.Spec.Rules = nil }, want: "missing the Tenant label"},
		{name: "rules lookup failure", change: func(f *volumeFixture) { f.failKind = "*v1beta2.RuleStatus" }, want: "injected read failure", statusReads: 1},
		{name: "missing rules status fallback", change: func(f *volumeFixture) { f.omitStatus = true }, statusReads: 1},
		{name: "fallback nonselected namespace", change: func(f *volumeFixture) { f.omitStatus = true; f.ns.Labels["profile"] = "ordinary" }, want: "missing the Tenant label", statusReads: 1},
		{name: "PV not found", change: func(f *volumeFixture) { f.omitPV = true }, want: "not yet existing PV"},
		{name: "PV read error", change: func(f *volumeFixture) { f.failKind = "*v1.PersistentVolume" }, want: "injected read failure"},
		{name: "deleting PV", change: func(f *volumeFixture) {
			now := metav1.Now()
			f.pv.DeletionTimestamp = &now
			f.pv.Finalizers = []string{"test"}
		}, want: "missing the Tenant label"},
		{name: "invalid PVC tenant selector", change: func(f *volumeFixture) { f.claim.Spec.Selector.MatchExpressions[0].Values = []string{"tenant-b"} }, want: "must contain only tenant"},
		{name: "bound skip", change: func(f *volumeFixture) { f.old.Status.Phase = corev1.ClaimBound }},
		{name: "dynamic skip", change: func(f *volumeFixture) { f.claim.Spec.VolumeName = ""; f.claim.Spec.Selector = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newVolumeFixture()
			if tc.change != nil {
				tc.change(f)
			}
			c := f.client(t)
			compiler, err := cache.NewCELCache()
			if err != nil {
				t.Fatal(err)
			}
			h := pvc.Handler(pvc.PersistentVolumeValidatingVolume(VolumeRules(nil, cache.NewLabelSelectorCache(), compiler)))
			handle := h.OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
			if f.req.Operation == admissionv1.Create {
				handle = h.OnCreate(c, c, admission.NewDecoder(c.Scheme()), nil)
			}
			beforePV, beforeClaim, beforeBody := f.pv.DeepCopy(), f.claim.DeepCopy(), f.body.DeepCopy()
			response := handle(t.Context(), f.request(t))
			if tc.want == "" {
				if response != nil {
					t.Fatalf("unexpected response: %#v", response)
				}
			} else if response == nil || response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, tc.want) {
				t.Fatalf("response=%#v, want %q", response, tc.want)
			}
			if c.gets["*v1beta2.RuleStatus"] != tc.statusReads {
				t.Fatalf("reads=%v, expected %d ruleset reads", c.gets, tc.statusReads)
			}
			if c.gets["*v1.PersistentVolume"] > 1 {
				t.Fatalf("redundant PV reads: %v", c.gets)
			}
			if !reflect.DeepEqual(beforePV, f.pv) || !reflect.DeepEqual(beforeClaim, f.claim) || !reflect.DeepEqual(beforeBody, f.body) {
				t.Fatal("admission mutated input labels, selector or rule")
			}
			if c.readPV != nil && !reflect.DeepEqual(c.readPV.Labels, beforePV.Labels) {
				t.Fatal("rule selector changed PV labels")
			}
		})
	}
}

type volumeTail struct {
	handlers.TypedHandlerWithTenant[*corev1.PersistentVolumeClaim]
	called bool
}

func (t *volumeTail) OnUpdate(client.Client, client.Reader, *corev1.PersistentVolumeClaim, *corev1.PersistentVolumeClaim, admission.Decoder, events.EventRecorder, *capsule.Tenant) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		t.called = true
		return ad.Deny("subsequent validation")
	}
}
func TestVolumeAllowContinuesValidation(t *testing.T) {
	f := newVolumeFixture()
	c := f.client(t)
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	tail := &volumeTail{}
	h := pvc.Handler(pvc.PersistentVolumeValidatingVolume(VolumeRules(nil, cache.NewLabelSelectorCache(), compiler)), tail)
	response := h.OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)(t.Context(), f.request(t))
	if !tail.called || response == nil || response.Allowed || response.Result.Message != "subsequent validation" {
		t.Fatalf("tail=%v response=%#v", tail.called, response)
	}
}

func TestVolumeRulesReadCurrentLabels(t *testing.T) {
	f := newVolumeFixture()
	c := f.client(t)
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	h := pvc.Handler(pvc.PersistentVolumeValidatingVolume(VolumeRules(nil, cache.NewLabelSelectorCache(), compiler)))
	handle := h.OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
	req := f.request(t)
	if response := handle(t.Context(), req); response != nil {
		t.Fatalf("initial response=%#v", response)
	}
	current := &corev1.PersistentVolume{}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(f.pv), current); err != nil {
		t.Fatal(err)
	}
	current.Labels[meta.TenantLabel] = "tenant-b"
	if err := c.Update(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	if response := handle(t.Context(), req); response == nil || response.Allowed {
		t.Fatal("cached authorization allowed a PV now owned by another tenant")
	}
}
