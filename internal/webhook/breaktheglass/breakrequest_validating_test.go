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
	authenticationv1 "k8s.io/api/authentication/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mc "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/internal/webhook/test"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	runtimeconfiguration "github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

type breakRequestValidationTestConfiguration struct {
	runtimeconfiguration.Configuration
	administrators rbac.UserListSpec
}

func (c breakRequestValidationTestConfiguration) Administrators() rbac.UserListSpec {
	return c.administrators
}

func TestBreakRequestValidationHandler(t *testing.T) {
	defaultTemplateName := "foo"
	alternateTemplateName := "bar"
	ctx := context.Background()
	log := ctrl.Log.WithName("test")
	templateRef := func(name string) capsulev1beta2.GlobalBreakRequestTemplateReference {
		return capsulev1beta2.GlobalBreakRequestTemplateReference{
			Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
			Name: name,
		}
	}
	localTemplateRef := func(name string) capsulev1beta2.BreakRequestTemplateReference {
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
				name: "deny parameters which do not match the template schema",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
						Params:   &runtime.RawExtension{Raw: []byte(`{"clusterRole":"admin:sad"}`)},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.ParamSchema = &runtime.RawExtension{Raw: []byte(`{
								"type":"object",
								"required":["clusterRole"],
								"properties":{"clusterRole":{"type":"string","pattern":"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$"}}
							}`)}
						})
				},
				expected: http.StatusForbidden,
				errMsg:   "parameters for template foo are invalid",
			},
			{
				name: "deny missing required template parameters",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: templateRef(defaultTemplateName),
						Params:   &runtime.RawExtension{Raw: []byte(`{}`)},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.ParamSchema = &runtime.RawExtension{Raw: []byte(`{
								"type":"object",
								"required":["clusterRole"],
								"properties":{"clusterRole":{"type":"string"}}
							}`)}
						})
				},
				expected: http.StatusForbidden,
				errMsg:   "clusterRole in body is required",
			},
			{
				name: "allow a namespace-local template",
				br: &capsulev1beta2.BreakRequest{
					ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
					Spec: capsulev1beta2.BreakRequestSpec{
						Template: localTemplateRef(alternateTemplateName),
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(
							gm.Any(),
							client.ObjectKey{Namespace: "team-a", Name: alternateTemplateName},
							gm.AssignableToTypeOf(&capsulev1beta2.BreakRequestTemplate{}),
						).
						Return(nil)
				},
				expected: 0,
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
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.Approvals = breaktheglass.ApprovalSpec{
								Auto:       true,
								Approvers:  rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "timmy"}},
								Conditions: []string{`requestor.name == "alice" && "developers" in requestor.groups`},
							}
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
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.Approvals = breaktheglass.ApprovalSpec{
								Auto:       true,
								Conditions: []string{`requestor.name == "alice"`},
							}
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
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.MaxDuration = &metav1.Duration{Duration: time.Minute}
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
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
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
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
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
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
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
						Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
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

	t.Run("OnDelete", func(t *testing.T) {
		future := metav1.NewTime(time.Now().Add(time.Hour))
		past := metav1.NewTime(time.Now().Add(-time.Hour))

		tests := []struct {
			name     string
			br       *capsulev1beta2.BreakRequest
			request  admission.Request
			expected int32
			errMsg   string
		}{
			{
				name:     "deny a request which has not entered a lifecycle phase",
				br:       &capsulev1beta2.BreakRequest{},
				expected: http.StatusForbidden,
				errMsg:   "cannot be deleted before it has expired (current phase: Initializing)",
			},
			{
				name: "allow a created request",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseCreated,
				}},
			},
			{
				name: "allow a requested request",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseRequested,
				}},
			},
			{
				name: "allow a pending request",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhasePending,
				}},
			},
			{
				name: "deny an active request",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseActive,
				}},
				expected: http.StatusForbidden,
				errMsg:   "cannot be deleted before it has expired (current phase: Active)",
			},
			{
				name: "allow an administrator to delete an active request",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseActive,
				}},
				request: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					UserInfo: authenticationv1.UserInfo{Username: "alice"},
				}},
			},
			{
				name: "allow an expired request without archive retention",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseExpired,
				}},
			},
			{
				name: "deny an expired request during archive retention",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase:     capsulev1beta2.RequestPhaseExpired,
					KeepUntil: &future,
				}},
				expected: http.StatusForbidden,
				errMsg:   "cannot be deleted before archive retention expires",
			},
			{
				name: "allow an administrator to delete during archive retention",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase:     capsulev1beta2.RequestPhaseExpired,
					KeepUntil: &future,
				}},
				request: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					UserInfo: authenticationv1.UserInfo{Username: "alice"},
				}},
			},
			{
				name: "allow an expired request after archive retention",
				br: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Phase:     capsulev1beta2.RequestPhaseExpired,
					KeepUntil: &past,
				}},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{OldObject: tt.br}
				validator := BreakRequestValidationHandler(log, breakRequestValidationTestConfiguration{
					administrators: rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}},
				})

				resp := validator.OnDelete(nil, nil, decoder, nil)(ctx, tt.request)
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
				errMsg:   "template cannot be changed. old: GlobalBreakRequestTemplate/foo, new: GlobalBreakRequestTemplate/bar",
			},
			{
				name: "deny changes to rendered resources by a reviewer",
				oldBr: &capsulev1beta2.BreakRequest{
					Status: capsulev1beta2.BreakRequestStatus{Request: &capsulev1beta2.BreakRequestStatusRequest{
						Resources: []apiruntime.RenderedResource{{
							Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"original"}}`)}},
						}},
					}},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Status: capsulev1beta2.BreakRequestStatus{Request: &capsulev1beta2.BreakRequestStatusRequest{
						Resources: []apiruntime.RenderedResource{{
							Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"injected"}}`)}},
						}},
					}},
				},
				request:  admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "status"}},
				expected: http.StatusForbidden,
				errMsg:   "rendered resources can only be changed by the Capsule controller",
			},
			{
				name: "deny changes to resolved impersonation by a reviewer",
				oldBr: &capsulev1beta2.BreakRequest{
					Status: capsulev1beta2.BreakRequestStatus{
						Request: &capsulev1beta2.BreakRequestStatusRequest{Impersonation: &capsulemeta.NamespacedRFC1123ObjectReferenceWithNamespace{
							Name:      "template-runner",
							Namespace: "operations",
						}},
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Status: capsulev1beta2.BreakRequestStatus{
						Request: &capsulev1beta2.BreakRequestStatusRequest{Impersonation: &capsulemeta.NamespacedRFC1123ObjectReferenceWithNamespace{
							Name:      "privileged-runner",
							Namespace: "kube-system",
						}},
					},
				},
				request:  admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "status"}},
				expected: http.StatusForbidden,
				errMsg:   "resolved impersonation can only be changed by the Capsule controller",
			},
			{
				name: "deny changes to resolved template by a reviewer",
				oldBr: &capsulev1beta2.BreakRequest{
					Status: capsulev1beta2.BreakRequestStatus{
						Request: &capsulev1beta2.BreakRequestStatusRequest{Template: &capsulev1beta2.ResolvedBreakRequestTemplateReference{
							BreakRequestTemplateReference: templateRef(defaultTemplateName),
							ResourceVersion:               "1234",
						}},
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Status: capsulev1beta2.BreakRequestStatus{
						Request: &capsulev1beta2.BreakRequestStatusRequest{Template: &capsulev1beta2.ResolvedBreakRequestTemplateReference{
							BreakRequestTemplateReference: templateRef(defaultTemplateName),
							ResourceVersion:               "5678",
						}},
					},
				},
				request:  admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "status"}},
				expected: http.StatusForbidden,
				errMsg:   "resolved template can only be changed by the Capsule controller",
			},
			{
				name: "deny changes to resolved approvals by a reviewer",
				oldBr: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Request: &capsulev1beta2.BreakRequestStatusRequest{Approvals: &breaktheglass.ApprovalSpec{
						Conditions: []string{"true"},
					}},
				}},
				newBr: &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
					Request: &capsulev1beta2.BreakRequestStatusRequest{Approvals: &breaktheglass.ApprovalSpec{
						Conditions: []string{"false"},
					}},
				}},
				request:  admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "status"}},
				expected: http.StatusForbidden,
				errMsg:   "resolved approvals can only be changed by the Capsule controller",
			},
			{
				name: "deny status changes without a lifecycle transition",
				oldBr: &capsulev1beta2.BreakRequest{
					Status: capsulev1beta2.BreakRequestStatus{
						Phase: capsulev1beta2.RequestPhaseRequested,
						Review: &capsulev1beta2.ReviewInfo{
							Verdict: capsulev1beta2.RequestVerdictPending,
						},
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Status: capsulev1beta2.BreakRequestStatus{
						Phase: capsulev1beta2.RequestPhaseRequested,
						Review: &capsulev1beta2.ReviewInfo{
							Verdict: capsulev1beta2.RequestVerdictApproved,
						},
					},
				},
				request:  admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "status"}},
				expected: http.StatusForbidden,
				errMsg:   "BreakRequest status can only be changed by requesting a lifecycle transition",
			},
			{
				name: "deny approval while rendering is not ready",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:    capsulemeta.ReadyCondition,
							Status:  metav1.ConditionFalse,
							Message: "rendering resource 0 failed",
						}},
						Phase: capsulev1beta2.RequestPhaseRequested,
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Phase:   capsulev1beta2.RequestPhaseApproved,
					},
				},
				request:  admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "status"}},
				expected: http.StatusForbidden,
				errMsg:   "cannot approve BreakRequest: rendered resources are not ready: rendering resource 0 failed",
			},
			{
				name: "allow approval when reviewer condition matches",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase:   capsulev1beta2.RequestPhaseRequested,
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:   capsulemeta.ReadyCondition,
							Status: metav1.ConditionTrue,
						}},
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase:   capsulev1beta2.RequestPhaseApproved,
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:   capsulemeta.ReadyCondition,
							Status: metav1.ConditionTrue,
						}},
						Review: &capsulev1beta2.ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
							Name:   "alice",
							Groups: []string{"reviewers"},
						}},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.Approvals.Conditions = []string{`"reviewers" in reviewer.groups`}
						})
				},
				request: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					SubResource: "status",
					UserInfo: authenticationv1.UserInfo{
						Username: "alice",
						Groups:   []string{"reviewers"},
					},
				}},
				expected: 0,
			},
			{
				name: "allow approval using the captured policy after the template changes",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase: capsulev1beta2.RequestPhaseRequested,
						Request: &capsulev1beta2.BreakRequestStatusRequest{Approvals: &breaktheglass.ApprovalSpec{
							Approvers:  rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}},
							Conditions: []string{"true"},
						}},
						Conditions: []metav1.Condition{{Type: capsulemeta.ReadyCondition, Status: metav1.ConditionTrue}},
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase: capsulev1beta2.RequestPhaseApproved,
						Request: &capsulev1beta2.BreakRequestStatusRequest{Approvals: &breaktheglass.ApprovalSpec{
							Approvers:  rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}},
							Conditions: []string{"true"},
						}},
						Conditions: []metav1.Condition{{Type: capsulemeta.ReadyCondition, Status: metav1.ConditionTrue}},
						Review: &capsulev1beta2.ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
							Name: "alice",
							Type: breaktheglass.AccessEntityTypeUser,
						}},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.Approvals = breaktheglass.ApprovalSpec{
								Approvers:  rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "bob"}},
								Conditions: []string{"false"},
							}
						})
				},
				request: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					SubResource: "status",
					UserInfo:    authenticationv1.UserInfo{Username: "alice"},
				}},
				expected: 0,
			},
			{
				name: "deny approval when reviewer condition does not match",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase:   capsulev1beta2.RequestPhaseRequested,
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:   capsulemeta.ReadyCondition,
							Status: metav1.ConditionTrue,
						}},
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase:   capsulev1beta2.RequestPhaseApproved,
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:   capsulemeta.ReadyCondition,
							Status: metav1.ConditionTrue,
						}},
						Review: &capsulev1beta2.ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
							Name:   "bob",
							Groups: []string{"users"},
						}},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.Approvals.Conditions = []string{`"reviewers" in reviewer.groups`}
						})
				},
				request: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					SubResource: "status",
					UserInfo: authenticationv1.UserInfo{
						Username: "bob",
						Groups:   []string{"users"},
					},
				}},
				expected: http.StatusForbidden,
				errMsg:   "approval conditions not satisfied",
			},
			{
				name: "allow an explicit group approver when one OR condition matches",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase:   capsulev1beta2.RequestPhaseRequested,
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:   capsulemeta.ReadyCondition,
							Status: metav1.ConditionTrue,
						}},
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase:   capsulev1beta2.RequestPhaseApproved,
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:   capsulemeta.ReadyCondition,
							Status: metav1.ConditionTrue,
						}},
						Review: &capsulev1beta2.ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
							Name:   "alice",
							Groups: []string{"on-call"},
						}},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.Approvals = breaktheglass.ApprovalSpec{
								Approvers: rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "on-call"}},
								Conditions: []string{
									`request.spec.reason == "emergency"`,
									`"on-call" in reviewer.groups`,
								},
							}
						})
				},
				request: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					SubResource: "status",
					UserInfo: authenticationv1.UserInfo{
						Username: "alice",
						Groups:   []string{"on-call"},
					},
				}},
				expected: 0,
			},
			{
				name: "deny a subject absent from approvers even when a condition matches",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase:   capsulev1beta2.RequestPhaseRequested,
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:   capsulemeta.ReadyCondition,
							Status: metav1.ConditionTrue,
						}},
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{Template: templateRef(defaultTemplateName)},
					Status: capsulev1beta2.BreakRequestStatus{
						Phase:   capsulev1beta2.RequestPhaseApproved,
						Request: &capsulev1beta2.BreakRequestStatusRequest{},
						Conditions: []metav1.Condition{{
							Type:   capsulemeta.ReadyCondition,
							Status: metav1.ConditionTrue,
						}},
						Review: &capsulev1beta2.ReviewInfo{Reviewer: &breaktheglass.AccessEntity{
							Name:   "bob",
							Groups: []string{"on-call"},
						}},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
							brt.Spec.Approvals = breaktheglass.ApprovalSpec{
								Approvers:  rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}},
								Conditions: []string{`"on-call" in reviewer.groups`},
							}
						})
				},
				request: admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					SubResource: "status",
					UserInfo: authenticationv1.UserInfo{
						Username: "bob",
						Groups:   []string{"on-call"},
					},
				}},
				expected: http.StatusForbidden,
				errMsg:   `subject "bob" is not permitted to approve requests for template foo`,
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

func TestBreakRequestValidationRejectsInvalidParametersBeforeRendering(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	brt := &capsulev1beta2.GlobalBreakRequestTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "context-template"},
		Spec: capsulev1beta2.GlobalBreakRequestTemplateSpec{
			ParamSchema: &runtime.RawExtension{Raw: []byte(`{"type":"object","required":["source"],"properties":{"source":{"type":"string"}}}`)},
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
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(brt).Build()

	br := &capsulev1beta2.BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
		Spec: capsulev1beta2.BreakRequestSpec{
			Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
				Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
				Name: brt.Name,
			},
			// This is intentionally missing the required source parameter. Parameter
			// schemas are enforced before context loading or rendering can start.
			Params: &runtime.RawExtension{Raw: []byte(`{}`)},
		},
	}
	decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{Object: br}
	validator := BreakRequestValidationHandler(ctrl.Log.WithName("test"), nil)

	resp := validator.OnCreate(cl, cl, decoder, nil)(context.Background(), admission.Request{})
	test.VerifyResponse(t, resp, http.StatusForbidden, "source in body is required")
}
