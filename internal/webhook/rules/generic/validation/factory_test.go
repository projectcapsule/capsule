// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestGenericRules(t *testing.T) {
	t.Parallel()

	got := GenericRules(nil)

	h, ok := got.(*genericRules)
	if !ok {
		t.Fatalf("expected *genericRules, got %T", got)
	}

	if h.regexCache == nil {
		t.Fatalf("expected regex cache")
	}

	if len(h.rules) != 1 {
		t.Fatalf("expected one generic validator, got %d", len(h.rules))
	}

	if !h.managedMetadata.HasLabel(meta.TenantLabel) {
		t.Fatalf("expected default managed metadata to include %q", meta.TenantLabel)
	}

	if len(h.objectSkipRules) == 0 {
		t.Fatalf("expected default object skip rules")
	}
}

func TestEvaluateGenericRules(t *testing.T) {
	t.Parallel()

	t.Run("nil object returns nil", func(t *testing.T) {
		t.Parallel()

		got, err := evaluateGenericRules[runtime.ExpressionMatch](
			nil,
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			genericRuleSet[runtime.ExpressionMatch]{},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil evaluation, got %#v", got)
		}
	})

	t.Run("empty enforce bodies returns nil", func(t *testing.T) {
		t.Parallel()

		got, err := evaluateGenericRules[runtime.ExpressionMatch](
			genericMetadataObject(nil, nil),
			nil,
			genericRuleSet[runtime.ExpressionMatch]{},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil evaluation, got %#v", got)
		}
	})

	t.Run("delegates to ruleengine", func(t *testing.T) {
		t.Parallel()

		set := genericRuleSet[runtime.ExpressionMatch]{
			Name:               "test",
			EventReason:        events.ReasonForbiddenMetadata,
			AllowedDescription: "Allowed test values",
			Values: func(obj genericObject) []ruleengine.Value {
				return []ruleengine.Value{
					{
						Value: obj.GetLabels()["env"],
						Path:  `metadata.labels["env"]`,
					},
				}
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []runtime.ExpressionMatch {
				return []runtime.ExpressionMatch{
					{
						Exact: []string{"prod"},
					},
				}
			},
			Matches: func(match runtime.ExpressionMatch, value ruleengine.Value) (ruleengine.Match, error) {
				return ruleengine.Match{
					Matched: value.Value == "prod",
				}, nil
			},
			RuleDescription: func(match runtime.ExpressionMatch) string {
				return runtime.DescribeExpressionMatch(match)
			},
		}

		got, err := evaluateGenericRules(
			genericMetadataObject(map[string]string{"env": "prod"}, nil),
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
			set,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got == nil {
			t.Fatalf("expected evaluation")
		}
		if err := got.BlockingError(); err != nil {
			t.Fatalf("expected no blocking error, got %v", err)
		}
	})
}

func TestValidateGenericRules(t *testing.T) {
	t.Parallel()

	baseGVK := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	}

	t.Run("nil object returns nil and does not call validators", func(t *testing.T) {
		t.Parallel()

		h := &genericRules{
			rules: []genericRuleValidator{
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					t.Fatalf("validator must not be called")

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			nil,
			baseGVK,
			testTenant(),
			testEventRecorder{},
			nil,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("sets gvk before running validators", func(t *testing.T) {
		t.Parallel()

		obj := genericMetadataObject(nil, nil)

		h := &genericRules{
			rules: []genericRuleValidator{
				func(obj genericObject, got schema.GroupVersionKind, _ []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					if got != baseGVK {
						t.Fatalf("expected gvk %s, got %s", baseGVK.String(), got.String())
					}

					if obj.GetObjectKind().GroupVersionKind() != baseGVK {
						t.Fatalf(
							"expected object gvk %s, got %s",
							baseGVK.String(),
							obj.GetObjectKind().GroupVersionKind().String(),
						)
					}

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			obj,
			baseGVK,
			testTenant(),
			testEventRecorder{},
			nil,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("skips controller managed objects before validators", func(t *testing.T) {
		t.Parallel()

		called := false
		skipRules := meta.DefaultObjectSkipRules()

		if len(skipRules) == 0 {
			t.Fatalf("expected default object skip rules")
		}

		labels := map[string]string{}
		for key, value := range skipRules[0].Labels {
			labels[key] = value
		}

		if len(labels) == 0 {
			t.Fatalf("expected default object skip rule to contain labels")
		}

		h := &genericRules{
			rules: []genericRuleValidator{
				func(
					genericObject,
					schema.GroupVersionKind,
					[]*apirules.NamespaceRuleEnforceBody,
				) (*ruleengine.Evaluation, error) {
					called = true

					return nil, nil
				},
			},
			regexCache:      cache.NewRegexCache(),
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: skipRules,
		}

		obj := genericMetadataObject(labels, nil)

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			obj,
			coreGVK("ConfigMap"),
			&capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "tenant-a",
				},
			},
			testEventRecorder{},
			[]*apirules.NamespaceRuleEnforceBody{
				{
					Action: apirules.ActionTypeAllow,
				},
			},
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if called {
			t.Fatalf("validator must not be called for skipped object")
		}
	})

	t.Run("does not skip non matching controller managed label value", func(t *testing.T) {
		t.Parallel()

		called := false

		h := &genericRules{
			rules: []genericRuleValidator{
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					called = true

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			genericMetadataObject(map[string]string{
				"managed-by": "human",
			}, nil),
			baseGVK,
			testTenant(),
			testEventRecorder{},
			nil,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !called {
			t.Fatalf("expected validator to be called")
		}
	})

	t.Run("runs validators in order", func(t *testing.T) {
		t.Parallel()

		calls := []string{}

		h := &genericRules{
			rules: []genericRuleValidator{
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					calls = append(calls, "first")

					return nil, nil
				},
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					calls = append(calls, "second")

					return &ruleengine.Evaluation{}, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			genericMetadataObject(nil, nil),
			baseGVK,
			testTenant(),
			testEventRecorder{},
			nil,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if got, want := strings.Join(calls, ","), "first,second"; got != want {
			t.Fatalf("expected calls %q, got %q", want, got)
		}
	})

	t.Run("passes enforce bodies to validators", func(t *testing.T) {
		t.Parallel()

		enforceBodies := []*apirules.NamespaceRuleEnforceBody{
			{
				Action: apirules.ActionTypeAllow,
			},
			{
				Action: apirules.ActionTypeDeny,
			},
		}

		h := &genericRules{
			rules: []genericRuleValidator{
				func(_ genericObject, _ schema.GroupVersionKind, got []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					if len(got) != len(enforceBodies) {
						t.Fatalf("expected %d enforce bodies, got %d", len(enforceBodies), len(got))
					}

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			genericMetadataObject(nil, nil),
			baseGVK,
			testTenant(),
			testEventRecorder{},
			enforceBodies,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("propagates validator error and stops", func(t *testing.T) {
		t.Parallel()

		expected := errors.New("boom")
		secondCalled := false

		h := &genericRules{
			rules: []genericRuleValidator{
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					return nil, expected
				},
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					secondCalled = true

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			genericMetadataObject(nil, nil),
			baseGVK,
			testTenant(),
			testEventRecorder{},
			nil,
		)
		if !errors.Is(err, expected) {
			t.Fatalf("expected %v, got %v", expected, err)
		}
		if secondCalled {
			t.Fatalf("second validator must not be called")
		}
	})

	t.Run("handles audit-only evaluation and continues", func(t *testing.T) {
		t.Parallel()

		calls := 0

		h := &genericRules{
			rules: []genericRuleValidator{
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					calls++

					return &ruleengine.Evaluation{
						Audits: []*ruleengine.Decision{
							{
								SetName:     "metadata label",
								EventReason: events.ReasonForbiddenMetadata,
								Action:      apirules.ActionTypeAudit,
								Value: ruleengine.Value{
									Value: "audit",
									Path:  `metadata.labels["audit"]`,
								},
								Message: "audit message",
							},
						},
					}, nil
				},
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					calls++

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			genericMetadataObject(nil, nil),
			baseGVK,
			testTenant(),
			testEventRecorder{},
			nil,
		)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if calls != 2 {
			t.Fatalf("expected two validators to be called, got %d", calls)
		}
	})

	t.Run("returns blocking evaluation error and stops", func(t *testing.T) {
		t.Parallel()

		secondCalled := false

		h := &genericRules{
			rules: []genericRuleValidator{
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					return &ruleengine.Evaluation{
						Blocking: &ruleengine.Decision{
							SetName:     "metadata label",
							EventReason: events.ReasonForbiddenMetadata,
							Action:      apirules.ActionTypeDeny,
							Value: ruleengine.Value{
								Value: "stage",
								Path:  `metadata.labels["env"]`,
							},
							Message: "blocked",
						},
					}, nil
				},
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					secondCalled = true

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		err := h.validateGenericRules(
			context.Background(),
			admission.Request{},
			genericMetadataObject(nil, nil),
			baseGVK,
			testTenant(),
			testEventRecorder{},
			nil,
		)
		if err == nil {
			t.Fatalf("expected blocking error")
		}

		var decisionErr *ruleengine.DecisionError
		if !errors.As(err, &decisionErr) {
			t.Fatalf("expected DecisionError, got %T: %v", err, err)
		}

		if decisionErr.Decision == nil || decisionErr.Decision.Message != "blocked" {
			t.Fatalf("unexpected decision error: %#v", decisionErr.Decision)
		}

		if secondCalled {
			t.Fatalf("second validator must not be called after blocking decision")
		}
	})
}

func TestGenericRulesOnCreate(t *testing.T) {
	t.Parallel()

	t.Run("allows when validators return nil", func(t *testing.T) {
		t.Parallel()

		called := false

		h := &genericRules{
			rules: []genericRuleValidator{
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					called = true

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		fn := h.OnCreate(
			nil,
			nil,
			genericMetadataObject(nil, nil),
			nil,
			testEventRecorder{},
			testTenant(),
			[]*apirules.NamespaceRuleBodyNamespace{
				{
					Enforce: &apirules.NamespaceRuleEnforceBody{
						Action: apirules.ActionTypeAllow,
					},
				},
			},
		)

		resp := fn(context.Background(), admissionRequest("v1", "ConfigMap"))
		if resp != nil {
			t.Fatalf("expected nil response, got %#v", resp)
		}
		if !called {
			t.Fatalf("expected validator to be called")
		}
	})

	t.Run("denies incomplete admission kind", func(t *testing.T) {
		t.Parallel()

		h := &genericRules{
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		fn := h.OnCreate(
			nil,
			nil,
			genericMetadataObject(nil, nil),
			nil,
			testEventRecorder{},
			testTenant(),
			nil,
		)

		resp := fn(context.Background(), admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Version: "",
					Kind:    "ConfigMap",
				},
			},
		})
		if resp == nil {
			t.Fatalf("expected denial response")
		}
		if resp.Allowed {
			t.Fatalf("expected denied response")
		}
		if !strings.Contains(resp.Result.Message, "admission request kind is incomplete") {
			t.Fatalf("expected incomplete kind error, got %q", resp.Result.Message)
		}
	})

	t.Run("denies validator error", func(t *testing.T) {
		t.Parallel()

		h := &genericRules{
			rules: []genericRuleValidator{
				func(genericObject, schema.GroupVersionKind, []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					return nil, errors.New("validator failed")
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		fn := h.OnCreate(
			nil,
			nil,
			genericMetadataObject(nil, nil),
			nil,
			testEventRecorder{},
			testTenant(),
			nil,
		)

		resp := fn(context.Background(), admissionRequest("v1", "ConfigMap"))
		if resp == nil {
			t.Fatalf("expected denial response")
		}
		if resp.Allowed {
			t.Fatalf("expected denied response")
		}
		if !strings.Contains(resp.Result.Message, "validator failed") {
			t.Fatalf("expected validator error, got %q", resp.Result.Message)
		}
	})
}

func TestGenericRulesOnUpdate(t *testing.T) {
	t.Parallel()

	t.Run("uses new object and allows when validators return nil", func(t *testing.T) {
		t.Parallel()

		calledWithNewObject := false

		oldObj := genericMetadataObject(map[string]string{"old": "true"}, nil)
		newObj := genericMetadataObject(map[string]string{"new": "true"}, nil)

		h := &genericRules{
			rules: []genericRuleValidator{
				func(obj genericObject, _ schema.GroupVersionKind, _ []*apirules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
					calledWithNewObject = obj.GetLabels()["new"] == "true"

					return nil, nil
				},
			},
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		fn := h.OnUpdate(
			nil,
			nil,
			oldObj,
			newObj,
			nil,
			testEventRecorder{},
			testTenant(),
			nil,
		)

		resp := fn(context.Background(), admissionRequest("v1", "ConfigMap"))
		if resp != nil {
			t.Fatalf("expected nil response, got %#v", resp)
		}
		if !calledWithNewObject {
			t.Fatalf("expected validator to receive the new object")
		}
	})

	t.Run("denies incomplete admission kind", func(t *testing.T) {
		t.Parallel()

		h := &genericRules{
			managedMetadata: meta.NewManagedMetadata(nil, nil),
			objectSkipRules: meta.DefaultObjectSkipRules(),
		}

		fn := h.OnUpdate(
			nil,
			nil,
			genericMetadataObject(nil, nil),
			genericMetadataObject(nil, nil),
			nil,
			testEventRecorder{},
			testTenant(),
			nil,
		)

		resp := fn(context.Background(), admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   "apps",
					Version: "v1",
					Kind:    "",
				},
			},
		})
		if resp == nil {
			t.Fatalf("expected denial response")
		}
		if resp.Allowed {
			t.Fatalf("expected denied response")
		}
		if !strings.Contains(resp.Result.Message, "admission request kind is incomplete") {
			t.Fatalf("expected incomplete kind error, got %q", resp.Result.Message)
		}
	})
}

func TestGenericRulesOnDelete(t *testing.T) {
	t.Parallel()

	h := &genericRules{}

	fn := h.OnDelete(
		nil,
		nil,
		genericMetadataObject(nil, nil),
		nil,
		testEventRecorder{},
		testTenant(),
		nil,
	)

	resp := fn(context.Background(), admissionRequest("v1", "ConfigMap"))
	if resp != nil {
		t.Fatalf("expected nil response, got %#v", resp)
	}
}

func TestGroupVersionKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     admission.Request
		want    schema.GroupVersionKind
		wantErr string
	}{
		{
			name: "core v1 kind",
			req:  admissionRequest("v1", "ConfigMap"),
			want: schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
		},
		{
			name: "grouped kind",
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
				},
			},
			want: schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
		},
		{
			name: "missing version",
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "",
						Kind:    "ConfigMap",
					},
				},
			},
			wantErr: "admission request kind is incomplete",
		},
		{
			name: "missing kind",
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "",
					},
				},
			},
			wantErr: "admission request kind is incomplete",
		},
		{
			name:    "empty request",
			req:     admission.Request{},
			wantErr: "admission request kind is incomplete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := groupVersionKind(tt.req)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want.String(), got.String())
			}
		})
	}
}

func genericMetadataObject(
	labels map[string]string,
	annotations map[string]string,
) genericObject {
	return &metav1.PartialObjectMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "object",
			Namespace:   "tenant-a",
			Labels:      labels,
			Annotations: annotations,
		},
	}
}

var (
	_ events.EventRecorder = testEventRecorder{}
	_ events.LabeledEvent  = (*testLabeledEvent)(nil)
)

type testEventRecorder struct{}

func (testEventRecorder) Eventf(
	k8sruntime.Object,
	k8sruntime.Object,
	string,
	string,
	string,
	string,
	...interface{},
) {
}

func (testEventRecorder) LabeledEvent(
	regarding k8sruntime.Object,
	eventType string,
	reason string,
	action string,
	note string,
) events.LabeledEvent {
	return &testLabeledEvent{
		regarding:   regarding,
		eventType:   eventType,
		reason:      reason,
		action:      action,
		note:        note,
		labels:      map[string]string{},
		annotations: map[string]string{},
	}
}

type testLabeledEvent struct {
	regarding k8sruntime.Object
	related   k8sruntime.Object

	eventType string
	reason    string
	action    string
	note      string

	labels      map[string]string
	annotations map[string]string
}

func (*testLabeledEvent) Emit(context.Context) {}

func (e *testLabeledEvent) WithRelated(obj k8sruntime.Object) events.LabeledEvent {
	e.related = obj

	return e
}

func (e *testLabeledEvent) WithLabels(labels map[string]string) events.LabeledEvent {
	for key, value := range labels {
		e.labels[key] = value
	}

	return e
}

func (e *testLabeledEvent) WithAnnotations(annotations map[string]string) events.LabeledEvent {
	for key, value := range annotations {
		e.annotations[key] = value
	}

	return e
}

func (e *testLabeledEvent) WithTenantLabel(tnt *capsulev1beta2.Tenant) events.LabeledEvent {
	if tnt != nil {
		e.labels[meta.NewTenantLabel] = tnt.Name
	}

	return e
}

func (e *testLabeledEvent) WithRequestAnnotations(req admission.Request) events.LabeledEvent {
	if req.UID != "" {
		e.annotations[meta.AuditRequestUID] = string(req.UID)
	}

	if req.UserInfo.Username != "" {
		e.annotations[meta.AuditUsername] = req.UserInfo.Username
	}

	return e
}

func (e *testLabeledEvent) Reason() string {
	return e.reason
}

func (e *testLabeledEvent) Action() string {
	return e.action
}

func (e *testLabeledEvent) Regarding() k8sruntime.Object {
	return e.regarding
}

func (e *testLabeledEvent) Labels() map[string]string {
	return e.labels
}

func (e *testLabeledEvent) Annotations() map[string]string {
	return e.annotations
}

func (e *testLabeledEvent) Note() string {
	return e.note
}

func (e *testLabeledEvent) EventType() string {
	return e.eventType
}

func (e *testLabeledEvent) Related() k8sruntime.Object {
	return e.related
}

func admissionRequest(
	apiVersion string,
	kind string,
) admission.Request {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		panic(err)
	}

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   gv.Group,
				Version: gv.Version,
				Kind:    kind,
			},
		},
	}
}

func testTenant() *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a",
		},
	}
}

func TestGenericRulesWithRealMetadataValidatorSmoke(t *testing.T) {
	t.Parallel()

	h := GenericRules(cache.NewRegexCache()).(*genericRules)

	evaluation, err := h.validateMetadata(
		genericMetadataObject(map[string]string{"env": "prod"}, nil),
		schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		},
		[]*apirules.NamespaceRuleEnforceBody{
			{
				Action: apirules.ActionTypeAllow,
				Metadata: []apirules.MetadataRule{
					{
						VersionKinds: gvkVersionKinds([]string{"*"}, "ConfigMap"),
						Labels: map[string]apirules.MetadataValueRule{
							"env": {
								Required: true,
								Values: []runtime.ExpressionMatch{
									{
										Exact: []string{"prod"},
									},
								},
							},
						},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if evaluation == nil {
		t.Fatalf("expected evaluation")
	}
	if err := evaluation.BlockingError(); err != nil {
		t.Fatalf("expected no blocking error, got %v", err)
	}
}

func gvkVersionKinds(apiVersion []string, kinds ...string) runtime.VersionKinds {
	return runtime.VersionKinds{
		APIGroups: apiVersion,
		Kinds:     kinds,
	}
}
