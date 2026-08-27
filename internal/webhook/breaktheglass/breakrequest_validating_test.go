// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	gm "go.uber.org/mock/gomock"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mc "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/internal/webhook/test"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

func TestBreakRequestValidationHandler(t *testing.T) {
	defaultTemplateName := "foo"
	alternateTemplateName := "bar"
	ctx := context.Background()
	log := ctrl.Log.WithName("test")
	templateRef := func(name string) capsulev1beta2.BreakRequestTemplateReference {
		return capsulev1beta2.BreakRequestTemplateReference{
			Kind: capsulev1beta2.BreakRequestTemplateKind,
			Name: name,
		}
	}

	t.Run("OnCreate", func(t *testing.T) {
		tests := []struct {
			name     string
			br       *capsulev1beta2.BreakRequest
			setup    func(reader *mc.MockReader)
			expected int32
			errMsg   string
		}{
			{
				name: "deny if template not found",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Return(&apierr.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonNotFound}})
				},
				expected: http.StatusForbidden,
				errMsg:   "template foo not found",
			},
			{
				name: "deny if template can not be loaded",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Return(errors.New("error loading template"))
				},
				expected: http.StatusInternalServerError,
				errMsg:   "error loading template foo: error loading template",
			},
			{
				name: "allow if template found",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(alternateTemplateName),
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: alternateTemplateName}, gm.Any()).
						Return(nil)
				},
				expected: 0, // allowed
			},
			{
				name: "allow auto approval when requestor condition matches",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
						Requestor: breaktheglass.AccessEntity{
							Name:   "alice",
							Groups: []string{"developers"},
						},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Spec.AutoApprove = true
							brt.Spec.ApprovalCondition = `requestor.name == "alice" && "developers" in requestor.groups`
						})
				},
				expected: 0,
			},
			{
				name: "deny auto approval when requestor condition does not match",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template:  templateRef(defaultTemplateName),
						Requestor: breaktheglass.AccessEntity{Name: "bob"},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Spec.AutoApprove = true
							brt.Spec.ApprovalCondition = `requestor.name == "alice"`
						})
				},
				expected: http.StatusForbidden,
				errMsg:   "approval conditions not satisfied",
			},
			{
				name: "deny if duration exceeds maxDuration",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
						Duration: &metav1.Duration{Duration: time.Hour},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Spec.MaxDuration.Duration = time.Minute
						})
				},
				expected: http.StatusForbidden,
				errMsg:   "requested duration 1h0m0s exceeds template maxDuration 1m0s",
			},
			{
				name: "allow template in a selected namespace",
				br: &capsulev1beta2.BreakRequest{
					ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Generation = 2
							brt.Spec.NamespaceSelectors = []selectors.NamespaceSelector{{
								LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"access": "enabled"}},
							}}
							brt.Status.ObservedGeneration = 2
							brt.Status.Namespaces = []string{"team-a"}
						})
				},
				expected: 0,
			},
			{
				name: "deny template outside selected namespaces",
				br: &capsulev1beta2.BreakRequest{
					ObjectMeta: metav1.ObjectMeta{Namespace: "team-b"},
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Generation = 2
							brt.Spec.NamespaceSelectors = []selectors.NamespaceSelector{{
								LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"access": "enabled"}},
							}}
							brt.Status.ObservedGeneration = 2
							brt.Status.Namespaces = []string{"team-a"}
						})
				},
				expected: http.StatusForbidden,
				errMsg:   "template foo is not available in namespace team-b",
			},
			{
				name: "deny while selected namespaces are stale",
				br: &capsulev1beta2.BreakRequest{
					ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Generation = 3
							brt.Spec.NamespaceSelectors = []selectors.NamespaceSelector{{
								LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"access": "enabled"}},
							}}
							brt.Status.ObservedGeneration = 2
							brt.Status.Namespaces = []string{"team-a"}
						})
				},
				expected: http.StatusForbidden,
				errMsg:   "template foo namespace selection is not ready",
			},
			{
				name: "deny if startTime is not in the future",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template:  templateRef(alternateTemplateName),
						StartTime: &metav1.Time{Time: time.Now().Add(-time.Minute)},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: alternateTemplateName}, gm.Any()).
						Return(nil)
				},
				expected: http.StatusForbidden,
				errMsg:   "must be in the future",
			},
			{
				name: "deny unsupported template kind",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: capsulev1beta2.BreakRequestTemplateReference{
							Kind: "OtherTemplate",
							Name: defaultTemplateName,
						},
					},
				},
				expected: http.StatusForbidden,
				errMsg:   `template kind "OtherTemplate" is not supported`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockCtrl := gm.NewController(t)
				defer mockCtrl.Finish()
				reader := mc.NewMockReader(mockCtrl)
				decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{
					Object: tt.br,
				}
				validator := BreakRequestValidationHandler(log, nil)

				if tt.setup != nil {
					tt.setup(reader)
				}

				resp := validator.OnCreate(nil, reader, decoder, nil)(ctx, admission.Request{})
				if tt.expected == 0 {
					assert.Nil(t, resp)
				} else {
					test.VerifyResponse(t, resp, tt.expected, tt.errMsg)
				}
			})
		}
	})

	t.Run("OnUpdate", func(t *testing.T) {
		tests := []struct {
			name     string
			oldBr    *capsulev1beta2.BreakRequest
			newBr    *capsulev1beta2.BreakRequest
			setup    func(reader *mc.MockReader)
			request  admission.Request
			expected int32
			errMsg   string
		}{
			{
				name: "allow if template not changed",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
					},
				},
				expected: 0,
			},
			{
				name: "deny if template changed",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(alternateTemplateName),
					},
				},
				expected: http.StatusForbidden,
				errMsg:   "template cannot be changed. old: BreakRequestTemplate/foo, new: BreakRequestTemplate/bar",
			},
			{
				name: "allow approval when reviewer condition matches",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec:   capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{Phase: capsulev1beta2.RequestPhaseRequested},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase: capsulev1beta2.RequestPhaseApproved,
						Review: &capsulev1beta2.ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
							Name:   "alice",
							Groups: []string{"reviewers"},
						}},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Spec.ApprovalCondition = `"reviewers" in reviewer.groups`
						})
				},
				request:  admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "status"}},
				expected: 0,
			},
			{
				name: "deny approval when reviewer condition does not match",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec:   capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{Phase: capsulev1beta2.RequestPhaseRequested},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase: capsulev1beta2.RequestPhaseApproved,
						Review: &capsulev1beta2.ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
							Name:   "bob",
							Groups: []string{"users"},
						}},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Spec.ApprovalCondition = `"reviewers" in reviewer.groups`
						})
				},
				request:  admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "status"}},
				expected: http.StatusForbidden,
				errMsg:   "approval conditions not satisfied",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockCtrl := gm.NewController(t)
				defer mockCtrl.Finish()
				reader := mc.NewMockReader(mockCtrl)
				decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{
					Object:    tt.newBr,
					OldObject: tt.oldBr,
				}
				validator := BreakRequestValidationHandler(log, nil)
				if tt.setup != nil {
					tt.setup(reader)
				}

				resp := validator.OnUpdate(nil, reader, decoder, nil)(ctx, tt.request)
				if tt.expected == 0 {
					assert.Nil(t, resp)
				} else {
					test.VerifyResponse(t, resp, tt.expected, tt.errMsg)
				}
			})
		}
	})
}

func TestBreakRequestValidationLoadsParameterizedContext(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	brt := &capsulev1beta2.BreakRequestTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "context-template"},
		Spec: capsulev1beta2.BreakRequestTemplateSpec{
			ParamSchema: runtime.RawExtension{Raw: []byte(`{"type":"object","required":["source"],"properties":{"source":{"type":"string"}}}`)},
			Context: &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
				ResourceReference: tpl.ResourceReference{
					VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
					Name:        "{{ .source }}",
				},
				Index: "settings",
			}}},
			Resources: []apiruntime.ResourceTemplate{{Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"rendered"},"data":{"value":"{{ (index .settings 0).data.value }}"}}`)}}}},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		brt,
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "source-config", Namespace: "team-a"},
			Data:       map[string]string{"value": "loaded"},
		},
	).Build()
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)

	br := &capsulev1beta2.BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
		Spec: capsulev1beta2.BreakRequestSpec{
			Template: capsulev1beta2.BreakRequestTemplateReference{
				Kind: capsulev1beta2.BreakRequestTemplateKind,
				Name: brt.Name,
			},
			Params: &runtime.RawExtension{Raw: []byte(`{"source":"source-config"}`)},
		},
	}
	decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{Object: br}
	validator := BreakRequestValidationHandler(ctrl.Log.WithName("test"), mapper)

	if resp := validator.OnCreate(cl, cl, decoder, nil)(context.Background(), admission.Request{}); resp != nil {
		t.Fatalf("expected request with loadable context to be allowed, got %#v", resp)
	}
}
