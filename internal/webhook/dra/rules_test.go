// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	resources "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func classBody(action rules.ActionType, matches ...rules.DeviceClassMatch) *rules.NamespaceRuleBodyTenant {
	return &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: action, Devices: rules.NamespaceRuleEnforceDevicesBody{Classes: matches}}}}
}

func setClassRules(tb testing.TB, cl client.Client, name string, bodies ...*rules.NamespaceRuleBodyTenant) {
	tb.Helper()
	tnt := &capsulev1beta2.Tenant{}
	require.NoError(tb, cl.Get(tb.Context(), client.ObjectKey{Name: name}, tnt))
	tnt.Spec.DeviceClasses = nil
	tnt.Spec.Rules = bodies
	require.NoError(tb, cl.Update(tb.Context(), tnt))
}

func TestNamespaceDeviceClassEnforcement(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "a"}}
	exact := rules.DeviceClassMatch{Exact: []string{"gpu-a"}}
	all := rules.DeviceClassMatch{Selector: &metav1.LabelSelector{}}
	for _, version := range []string{"v1", "v1beta2"} {
		for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
			for _, tc := range []struct {
				name     string
				rules    []*rules.NamespaceRuleBodyTenant
				requests []resources.DeviceRequest
				denied   string
				gets     int
			}{
				{name: "exact", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, exact)}, requests: exactRequests("gpu-a")},
				{name: "name miss", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, exact)}, requests: exactRequests("gpu-other"), denied: "not allowed"},
				{name: "absent selector does not match all", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, exact)}, requests: alternativeRequests("gpu-a", "gpu-b"), denied: "firstAvailable[1]"},
				{name: "selector", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Selector: selector})}, requests: exactRequests("gpu-a", "gpu-b"), gets: 2},
				{name: "selector missing class", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Selector: selector})}, requests: exactRequests("missing"), denied: "does not exist", gets: 1},
				{name: "selector rejects fallback", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Selector: selector})}, requests: alternativeRequests("gpu-a", "gpu-other"), denied: "firstAvailable[1]", gets: 2},
				{name: "all existing classes", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, all)}, requests: alternativeRequests("gpu-a", "gpu-other"), gets: 2},
				{name: "duplicate reads", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, all)}, requests: alternativeRequests("gpu-a", "gpu-a"), gets: 1},
				{name: "name or selector", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: []string{"gpu-other"}, Selector: selector})}, requests: exactRequests("gpu-a", "gpu-other"), gets: 2},
				{name: "regex", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Expression: "^gpu-[ab]$"})}, requests: alternativeRequests("gpu-a", "gpu-b")},
				{name: "negated name", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeDeny, rules.DeviceClassMatch{Exact: []string{"gpu-a"}, Negate: true})}, requests: exactRequests("gpu-other"), denied: "denied"},
				{name: "later allow", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeDeny, all), classBody(rules.ActionTypeAllow, exact)}, requests: exactRequests("gpu-a"), gets: 1},
				{name: "later deny", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, all), classBody(rules.ActionTypeDeny, exact)}, requests: exactRequests("gpu-a"), denied: "denied", gets: 1},
				{name: "audit grants nothing", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, exact), classBody(rules.ActionTypeAudit, all)}, requests: exactRequests("gpu-other"), denied: "not allowed", gets: 1},
				{name: "audit never denies", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAudit, exact)}, requests: exactRequests("gpu-a")},
				{name: "audit missing class", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAudit, all)}, requests: exactRequests("missing"), gets: 1},
				{name: "empty requests", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeDeny, all)}},
				{name: "missing form", rules: []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, exact)}, requests: []resources.DeviceRequest{{Name: "gpu"}}, denied: "deviceClassName is required"},
			} {
				t.Run(version+"/"+kind+"/"+tc.name, func(t *testing.T) {
					cl := newDRAClientVersion(t, version)
					setClassRules(t, cl, "tenant-a", tc.rules...)
					request := draAdmissionRequestVersion(t, version, kind, "ns-a", tc.requests)
					original := append([]byte(nil), request.Object.Raw...)
					response := DeviceClass(nil, nil, nil, nil).OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())(t.Context(), request)
					if tc.denied == "" {
						require.Nil(t, response)
					} else {
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						require.Contains(t, response.Result.Message, tc.denied)
					}
					require.EqualValues(t, tc.gets, cl.deviceGets.Load())
					require.Zero(t, cl.tenantLists.Load())
					require.Equal(t, original, request.Object.Raw)
				})
			}
		}
	}
}

func TestDeviceClassRuleScopeAndLifecycle(t *testing.T) {
	cl := newDRAClient(t)
	body := classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "a"}}})
	body.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "gpu"}}
	body.Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}
	setClassRules(t, cl, "tenant-a", body)
	setClassRules(t, cl, "tenant-b", classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: []string{"gpu-other"}}))
	ns := &corev1.Namespace{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "ns-a"}, ns))
	ns.Labels["profile"] = "gpu"
	require.NoError(t, cl.Update(t.Context(), ns))
	handler := DeviceClass(nil, nil, nil, nil).OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
	request := draAdmissionRequest(t, "ResourceClaim", "ns-a", exactRequests("gpu-other"))
	request.UserInfo = authenticationv1.UserInfo{Username: "alice"}
	require.NotNil(t, handler(t.Context(), request))
	request.UserInfo.Username = "bob"
	require.Nil(t, handler(t.Context(), request))
	request.UserInfo.Username = "alice"
	ns.Labels["profile"] = "other"
	require.NoError(t, cl.Update(t.Context(), ns))
	require.Nil(t, handler(t.Context(), request))
	ns.Labels["profile"] = "gpu"
	require.NoError(t, cl.Update(t.Context(), ns))
	class := &resources.DeviceClass{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "gpu-other"}, class))
	class.Labels["tenant"] = "a"
	require.NoError(t, cl.Update(t.Context(), class))
	require.Nil(t, handler(t.Context(), request))
	require.NoError(t, cl.Delete(t.Context(), class))
	require.NotNil(t, handler(t.Context(), request))
	class.ResourceVersion = ""
	require.NoError(t, cl.Create(t.Context(), class))
	require.Nil(t, handler(t.Context(), request))
	other := draAdmissionRequest(t, "ResourceClaim", "ns-b", exactRequests("gpu-a"))
	require.NotNil(t, handler(t.Context(), other), "destination tenant decides, regardless of caller")
	setClassRules(t, cl, "tenant-a", classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: []string{"gpu-a"}}))
	require.NotNil(t, handler(t.Context(), request))
}

func TestDeviceClassEffectiveStatusAndLegacyBoundary(t *testing.T) {
	for _, pending := range []bool{false, true} {
		cl := newDRAClient(t)
		body := classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: []string{"gpu-other"}})
		setClassRules(t, cl, "tenant-a", body)
		status := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: "ns-a", Generation: 2, Status: capsulev1beta2.RuleStatusStatus{}}
		// Use the published profile only after its observed generation catches up.
		status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{classBody(rules.ActionTypeDeny, rules.DeviceClassMatch{Exact: []string{"gpu-other"}}).NamespaceRuleBodyNamespace}
		status.Status.ObservedGeneration = 2
		if pending {
			status.Status.ObservedGeneration = 1
		}
		require.NoError(t, cl.Create(t.Context(), status))
		handler := DeviceClass(nil, nil, nil, nil).OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
		request := draAdmissionRequest(t, "ResourceClaim", "ns-a", exactRequests("gpu-other"))
		response := handler(t.Context(), request)
		if pending {
			require.Nil(t, response)
		} else {
			require.NotNil(t, response)
			require.Contains(t, response.Result.Message, "denied")
		}
		tnt := &capsulev1beta2.Tenant{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tnt))
		tnt.Spec.DeviceClasses = &api.SelectorAllowedListSpec{Exact: []string{"gpu-a"}}
		require.NoError(t, cl.Update(t.Context(), tnt))
		response = handler(t.Context(), request)
		require.NotNil(t, response)
		require.Contains(t, response.Result.Message, "Device Class gpu-other is forbidden")
	}
}

func TestDeviceClassConditionsErrorsAndEarlySkips(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	cl := newDRAClient(t)
	body := classBody(rules.ActionTypeDeny, rules.DeviceClassMatch{Selector: &metav1.LabelSelector{}})
	body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "request.operation == 'UPDATE'"}}
	setClassRules(t, cl, "tenant-a", body)
	h := DeviceClass(nil, nil, nil, compiler)
	request := draAdmissionRequest(t, "ResourceClaim", "ns-a", exactRequests("gpu-a"))
	require.Nil(t, h.OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())(t.Context(), request))
	require.Zero(t, cl.deviceGets.Load())
	body.Enforce.Conditions[0].Expression = "object.spec.missing == 'x'"
	setClassRules(t, cl, "tenant-a", body)
	response := h.OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())(t.Context(), request)
	require.NotNil(t, response)
	require.Contains(t, response.Result.Message, "conditions[0]")
	cl.getErrorRuleset = true
	response = h.OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())(t.Context(), request)
	require.NotNil(t, response)
	require.Contains(t, response.Result.Message, "ruleset lookup failed")
	// A request with stale ownership must not be treated as an unmanaged namespace.
	cl.getErrorRuleset = false
	ns := &corev1.Namespace{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "ns-a"}, ns))
	ns.OwnerReferences[0].UID = types.UID("stale")
	tnt := &capsulev1beta2.Tenant{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, tnt))
	tnt.UID = types.UID("current")
	require.NoError(t, cl.Update(t.Context(), tnt))
	require.NoError(t, cl.Update(t.Context(), ns))
	response = h.OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())(t.Context(), request)
	require.NotNil(t, response)
	require.Contains(t, response.Result.Message, "UID mismatch")
	for _, operation := range []admissionv1.Operation{admissionv1.Update, admissionv1.Delete} {
		request.Operation = operation
		require.Nil(t, h.OnUpdate(nil, nil, nil, nil)(t.Context(), request))
	}
	request.Operation = admissionv1.Create
	request.SubResource = "status"
	require.Nil(t, h.OnCreate(nil, nil, nil, nil)(t.Context(), request))
	request.SubResource = ""
	request.Kind.Group = "unrelated.example.com"
	require.Nil(t, h.OnCreate(nil, nil, nil, nil)(t.Context(), request))
}

func TestDeviceClassMalformedRequestTrees(t *testing.T) {
	for _, devices := range []string{`{"requests":{}}`, `{"requests":[1]}`, `{"requests":[{"exactly":1}]}`, `{"requests":[{"firstAvailable":{}}]}`, `{"requests":[{"firstAvailable":[1]}]}`} {
		cl := newDRAClient(t)
		setClassRules(t, cl, "tenant-a", classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: []string{"gpu-a"}}))
		request := draAdmissionRequest(t, "ResourceClaim", "ns-a", nil)
		var raw map[string]any
		require.NoError(t, json.Unmarshal(request.Object.Raw, &raw))
		var value any
		require.NoError(t, json.Unmarshal([]byte(devices), &value))
		raw["spec"] = map[string]any{"devices": value}
		request.Object.Raw, _ = json.Marshal(raw)
		response := DeviceClass(nil, nil, nil, nil).OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())(t.Context(), request)
		require.NotNil(t, response)
		require.False(t, response.Allowed)
	}
}

func TestDeviceClassSharedCachesAndControllerRequests(t *testing.T) {
	cl := newDRAClient(t)
	regex := cache.NewRegexCache()
	selectors := cache.NewLabelSelectorCache()
	for _, tenantName := range []string{"a", "b"} {
		pattern := "^gpu-a$"
		if tenantName == "b" {
			pattern = "^gpu-other$"
		}
		setClassRules(t, cl, "tenant-"+tenantName, classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: pattern}, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": tenantName}}}))
	}
	handler := DeviceClass(nil, regex, selectors, nil).OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), events.NewDiscardRecorder())
	failures := make(chan string, 80)
	var group sync.WaitGroup
	for i := range 40 {
		group.Go(func() {
			namespace, allowed, denied := "ns-a", "gpu-a", "gpu-other"
			if i%2 == 1 {
				namespace, allowed, denied = "ns-b", "gpu-other", "gpu-a"
			}
			for _, tc := range []struct {
				class   string
				blocked bool
			}{{allowed, false}, {denied, true}} {
				request := draAdmissionRequest(t, "ResourceClaim", namespace, exactRequests(tc.class))
				request.UserInfo.Username = "system:kube-scheduler"
				response := handler(t.Context(), request)
				if blocked := response != nil && !response.Allowed; blocked != tc.blocked {
					failures <- fmt.Sprintf("namespace=%s class=%s response=%+v", namespace, tc.class, response)
				}
			}
		})
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	require.Equal(t, 2, regex.Stats())
	regex.Reset()
	request := draAdmissionRequest(t, "ResourceClaim", "ns-a", exactRequests("gpu-a"))
	require.Nil(t, handler(t.Context(), request))
	require.Equal(t, 1, regex.Stats())
}
