// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func profilePod() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "tenant-a"}, Spec: corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}, AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeRuntimeDefault}},
		Containers:      []corev1.Container{{Name: "app", Image: "example.com/app:v1"}}, InitContainers: []corev1.Container{{Name: "init", Image: "example.com/init:v1", RestartPolicy: new(corev1.ContainerRestartPolicyAlways)}}, EphemeralContainers: []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug", Image: "example.com/debug:v1"}}},
	}}
}

func profileBody(appArmor bool, action rules.ActionType, types ...rules.SecurityProfileType) *rules.NamespaceRuleEnforceBody {
	b := &rules.NamespaceRuleEnforceBody{Action: action}
	match := []rules.WorkloadSecurityProfileMatch{{Types: types}}
	if appArmor {
		b.Workloads.AppArmorProfiles = match
	} else {
		b.Workloads.SeccompProfiles = match
	}
	return b
}

func profileContext(kind rules.SecurityProfileType, name string) *corev1.SecurityContext {
	c := &corev1.SecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileType(kind)}, AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileType(kind)}}
	if name != "" {
		c.SeccompProfile.LocalhostProfile = new(name)
		c.AppArmorProfile.LocalhostProfile = new(name)
	}
	return c
}

func TestSecurityProfileEffectiveValuesAndTargets(t *testing.T) {
	for _, appArmor := range []bool{false, true} {
		for _, tc := range []struct {
			name, want string
			targets    []rules.WorkloadValidationTarget
			change     func(*corev1.Pod)
		}{
			{name: "inherited", change: func(*corev1.Pod) {}},
			{name: "container override", want: "containers[0]", change: func(p *corev1.Pod) {
				p.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileUnconfined, "")
			}},
			{name: "init override", want: "initContainers[0]", change: func(p *corev1.Pod) {
				p.Spec.InitContainers[0].SecurityContext = profileContext(rules.SecurityProfileUnconfined, "")
			}},
			{name: "ephemeral override", want: "ephemeralContainers[0]", change: func(p *corev1.Pod) {
				p.Spec.EphemeralContainers[0].SecurityContext = profileContext(rules.SecurityProfileUnconfined, "")
			}},
			{name: "privileged", want: "Unconfined", change: func(p *corev1.Pod) {
				p.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileRuntimeDefault, "")
				p.Spec.Containers[0].SecurityContext.Privileged = new(true)
			}},
			{name: "no profiles", want: "Unset", change: func(p *corev1.Pod) { p.Spec.SecurityContext = nil }},
			{name: "all containers explicit", change: func(p *corev1.Pod) {
				p.Spec.SecurityContext = nil
				p.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileRuntimeDefault, "")
				p.Spec.InitContainers[0].SecurityContext = profileContext(rules.SecurityProfileRuntimeDefault, "")
				p.Spec.EphemeralContainers[0].SecurityContext = profileContext(rules.SecurityProfileRuntimeDefault, "")
			}},
			{name: "missing pod default explicitly selected", targets: []rules.WorkloadValidationTarget{rules.ValidatePod}, want: "spec.securityContext", change: func(p *corev1.Pod) { p.Spec.SecurityContext = nil }},
			{name: "pod target does not select overrides", targets: []rules.WorkloadValidationTarget{rules.ValidatePod}, change: func(p *corev1.Pod) {
				p.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileUnconfined, "")
			}},
			{name: "only init targeted", targets: []rules.WorkloadValidationTarget{rules.ValidateInitContainers}, change: func(p *corev1.Pod) {
				p.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileUnconfined, "")
			}},
			{name: "volumes irrelevant", targets: []rules.WorkloadValidationTarget{rules.ValidateVolumes}, change: func(p *corev1.Pod) { p.Spec.SecurityContext = nil }},
			{name: "Windows skipped", change: func(p *corev1.Pod) { p.Spec.OS = &corev1.PodOS{Name: corev1.Windows}; p.Spec.SecurityContext = nil }},
		} {
			t.Run(fmt.Sprintf("apparmor=%v/%s", appArmor, tc.name), func(t *testing.T) {
				p := profilePod()
				tc.change(p)
				before := p.DeepCopy()
				b := profileBody(appArmor, rules.ActionTypeAllow, rules.SecurityProfileRuntimeDefault)
				b.Workloads.Targets = tc.targets
				original := b.DeepCopy()
				got, err := newPodRules(nil, nil, nil).validateSecurityProfiles(p, []*rules.NamespaceRuleEnforceBody{nil, b}, appArmor)
				require.NoError(t, err)
				if tc.want == "" {
					require.NoError(t, got.BlockingError())
				} else {
					require.ErrorContains(t, got.BlockingError(), tc.want)
				}
				require.Equal(t, before, p)
				require.Equal(t, original, b)
			})
		}
	}
}

func TestAppArmorLegacyAnnotationPrecedence(t *testing.T) {
	for _, tc := range []struct {
		annotation string
		override   bool
		denied     bool
	}{
		{"runtime/default", false, false}, {"unconfined", false, true}, {"localhost/custom", false, true}, {"bad", false, true}, {"unconfined", true, false},
	} {
		t.Run(fmt.Sprintf("%s/override=%v", tc.annotation, tc.override), func(t *testing.T) {
			pod := profilePod()
			pod.Annotations = map[string]string{corev1.DeprecatedAppArmorBetaContainerAnnotationKeyPrefix + "app": tc.annotation}
			if tc.override {
				pod.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileRuntimeDefault, "")
			}
			got, err := newPodRules(nil, nil, nil).validateAppArmorProfiles(pod, []*rules.NamespaceRuleEnforceBody{profileBody(true, rules.ActionTypeAllow, rules.SecurityProfileRuntimeDefault)})
			require.NoError(t, err)
			require.Equal(t, tc.denied, got.BlockingError() != nil)
		})
	}
}

func TestSecurityProfileOrderingAndLocalhostMatching(t *testing.T) {
	for _, appArmor := range []bool{false, true} {
		t.Run(fmt.Sprint(appArmor), func(t *testing.T) {
			pod := profilePod()
			pod.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileLocalhost, "teams/a.json")
			allow := profileBody(appArmor, rules.ActionTypeAllow, rules.SecurityProfileRuntimeDefault, rules.SecurityProfileLocalhost)
			match := &allow.Workloads.SeccompProfiles
			if appArmor {
				match = &allow.Workloads.AppArmorProfiles
			}
			(*match)[0].LocalhostProfiles = []apiruntime.ExpressionMatch{{Exact: []string{"other"}}, {ExpressionRegex: apiruntime.ExpressionRegex{Expression: `^teams/a\.json$`}}}
			h := newPodRules(nil, nil, nil)
			deny := profileBody(appArmor, rules.ActionTypeDeny, rules.SecurityProfileLocalhost)
			audit := profileBody(appArmor, rules.ActionTypeAudit, rules.SecurityProfileLocalhost)
			for _, tc := range []struct {
				bodies []*rules.NamespaceRuleEnforceBody
				denied bool
				audits int
			}{
				{[]*rules.NamespaceRuleEnforceBody{allow}, false, 0}, {[]*rules.NamespaceRuleEnforceBody{allow, deny}, true, 0}, {[]*rules.NamespaceRuleEnforceBody{deny, allow, audit}, false, 1}, {[]*rules.NamespaceRuleEnforceBody{allow, deny, audit}, true, 1},
			} {
				got, err := h.validateSecurityProfiles(pod, tc.bodies, appArmor)
				require.NoError(t, err)
				require.Equal(t, tc.denied, got.BlockingError() != nil)
				require.Len(t, got.Audits, tc.audits)
			}
			require.Equal(t, 1, h.regexCache.Stats())
			h.regexCache.Reset()
			pod.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileLocalhost, "teams/b.json")
			got, err := h.validateSecurityProfiles(pod, []*rules.NamespaceRuleEnforceBody{allow, audit}, appArmor)
			require.NoError(t, err)
			require.ErrorContains(t, got.BlockingError(), "teams/b.json")
			require.Len(t, got.Audits, 1)
			require.Equal(t, 1, h.regexCache.Stats())
			(*match)[0].LocalhostProfiles[1].Negate = true
			got, err = h.validateSecurityProfiles(pod, []*rules.NamespaceRuleEnforceBody{allow}, appArmor)
			require.NoError(t, err)
			require.NoError(t, got.BlockingError())
			(*match)[0].LocalhostProfiles[1].Expression = "["
			_, err = h.validateSecurityProfiles(pod, []*rules.NamespaceRuleEnforceBody{allow}, appArmor)
			require.Error(t, err)
		})
	}
}

func TestSecurityProfileAdmissionConditionsAndSubresources(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	h := PodRules(nil, nil, compiler)
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	for _, sub := range []string{"", "ephemeralcontainers", "status", "resize"} {
		for _, expression := range []string{"true", "false", "object.spec.missing == 'x'"} {
			t.Run(sub+expression, func(t *testing.T) {
				pod := profilePod()
				pod.Spec.EphemeralContainers[0].SecurityContext = profileContext(rules.SecurityProfileUnconfined, "")
				b := profileBody(false, rules.ActionTypeAllow, rules.SecurityProfileRuntimeDefault)
				b.Conditions = []rules.AdmissionCondition{{Expression: expression}}
				req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Update, SubResource: sub}}
				got := h.OnUpdate(nil, nil, profilePod(), pod, nil, recorder, &capsule.Tenant{}, []*rules.NamespaceRuleBodyNamespace{{Enforce: b}})(t.Context(), req)
				denied := (sub == "" || sub == "ephemeralcontainers") && expression != "false"
				if denied {
					require.NotNil(t, got)
					require.False(t, got.Allowed)
					if expression == "true" {
						require.Contains(t, got.Result.Message, "ephemeralContainers")
					} else {
						require.Contains(t, got.Result.Message, "conditions")
					}
				} else {
					require.Nil(t, got)
				}
			})
		}
	}
}

func TestSecurityProfileWindowsSkipsConditions(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	pod := profilePod()
	pod.Spec.OS = &corev1.PodOS{Name: corev1.Windows}
	pod.Spec.SecurityContext = nil
	b := profileBody(false, rules.ActionTypeAllow, rules.SecurityProfileRuntimeDefault)
	b.Conditions = []rules.AdmissionCondition{{Expression: "object.spec.missing == 'x'"}}
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	response := PodRules(nil, nil, compiler).OnCreate(nil, nil, pod, nil, recorder, &capsule.Tenant{}, []*rules.NamespaceRuleBodyNamespace{{Enforce: b}})(t.Context(), admission.Request{})
	require.Nil(t, response)
}

func TestSecurityProfileControllerTemplates(t *testing.T) {
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	h := TemplateRules(nil, nil, nil)
	b := profileBody(false, rules.ActionTypeAllow, rules.SecurityProfileRuntimeDefault)
	b.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateCronJob}
	for _, kind := range []string{"Deployment", "CronJob", "StatefulSet"} {
		for _, allowed := range []bool{false, true} {
			kindType := "Unconfined"
			if allowed {
				kindType = "RuntimeDefault"
			}
			template := map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "example.com/app:v1"}}, "securityContext": map[string]any{"seccompProfile": map[string]any{"type": kindType}}}}
			group := "apps"
			spec := map[string]any{"template": template}
			if kind == "CronJob" {
				group = "batch"
				spec = map[string]any{"jobTemplate": map[string]any{"spec": spec}}
			}
			obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": group + "/v1", "kind": kind, "spec": spec}}
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind{Group: group, Version: "v1", Kind: kind}}}
			for _, update := range []bool{false, true} {
				call := h.OnCreate(nil, nil, obj, nil, recorder, &capsule.Tenant{}, []*rules.NamespaceRuleBodyNamespace{{Enforce: b}})
				if update {
					call = h.OnUpdate(nil, nil, obj.DeepCopy(), obj, nil, recorder, &capsule.Tenant{}, []*rules.NamespaceRuleBodyNamespace{{Enforce: b}})
				}
				got := call(t.Context(), req)
				if allowed || kind == "StatefulSet" {
					require.Nil(t, got)
				} else {
					require.NotNil(t, got)
					require.Contains(t, got.Result.Message, "spec.template")
					require.Contains(t, got.Result.Message, "seccomp profile")
				}
			}
		}
	}
}

func TestSecurityProfileConcurrentCacheUse(t *testing.T) {
	h := newPodRules(nil, nil, nil)
	b := profileBody(false, rules.ActionTypeAllow, rules.SecurityProfileLocalhost)
	b.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateContainers}
	b.Workloads.SeccompProfiles[0].LocalhostProfiles = []apiruntime.ExpressionMatch{{ExpressionRegex: apiruntime.ExpressionRegex{Expression: `^teams/a\.json$`}}}
	original := b.DeepCopy()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			p := profilePod()
			name := "teams/a.json"
			if i%2 == 1 {
				name = "teams/b.json"
			}
			p.Spec.Containers[0].SecurityContext = profileContext(rules.SecurityProfileLocalhost, name)
			got, err := h.validateSeccompProfiles(p, []*rules.NamespaceRuleEnforceBody{b})
			if err != nil || (got.BlockingError() != nil) != (i%2 == 1) {
				t.Errorf("name=%s result=%v error=%v", name, got, err)
			}
		})
	}
	wg.Wait()
	require.Equal(t, 1, h.regexCache.Stats())
	require.Equal(t, original, b)
}
