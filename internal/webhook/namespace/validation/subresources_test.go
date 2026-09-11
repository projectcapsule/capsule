// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/utils"
)

const podSecurityLabel = "pod-security.kubernetes.io/enforce"

// Exercise the validating entry point without mutation: Kubernetes invokes it
// directly for namespace subresources, including requests with changed metadata.
//
//nolint:staticcheck
func TestNamespaceMetadataAdmissionAcrossResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(*capsulev1beta2.Tenant, *corev1.Namespace, *corev1.Namespace)
		denial  string
	}{
		{
			name: "allowed owner label",
			prepare: func(_ *capsulev1beta2.Tenant, _, newNs *corev1.Namespace) {
				newNs.Labels["example.com/probe"] = "changed"
			},
		},
		{
			name: "foreign tenant",
			prepare: func(tnt *capsulev1beta2.Tenant, _, newNs *corev1.Namespace) {
				tnt.Status.Owners[0].Name = "bob"
				newNs.Labels["example.com/probe"] = "changed"
			},
			denial: "denied patch request for this namespace",
		},
		{
			name: "forbidden label",
			prepare: func(tnt *capsulev1beta2.Tenant, _, newNs *corev1.Namespace) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenLabels: api.ForbiddenListSpec{Exact: []string{podSecurityLabel}},
				}
				newNs.Labels[podSecurityLabel] = "privileged"
			},
			denial: "namespace labels validation failed",
		},
		{
			name: "forbidden annotation",
			prepare: func(tnt *capsulev1beta2.Tenant, _, newNs *corev1.Namespace) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenAnnotations: api.ForbiddenListSpec{Exact: []string{"example.com/protected"}},
				}
				newNs.Annotations["example.com/protected"] = "changed"
			},
			denial: "namespace annotations validation failed",
		},
		{
			name: "required label removal",
			prepare: func(tnt *capsulev1beta2.Tenant, _, newNs *corev1.Namespace) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					RequiredMetadata: &capsulev1beta2.RequiredMetadata{Labels: map[string]string{podSecurityLabel: "restricted"}},
				}
				delete(newNs.Labels, podSecurityLabel)
			},
			denial: "required label",
		},
		{
			name: "required annotation removal",
			prepare: func(tnt *capsulev1beta2.Tenant, _, newNs *corev1.Namespace) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					RequiredMetadata: &capsulev1beta2.RequiredMetadata{Annotations: map[string]string{"example.com/protected": "original"}},
				}
				delete(newNs.Annotations, "example.com/protected")
			},
			denial: "required annotation",
		},
		{
			name: "metadata rule",
			prepare: func(tnt *capsulev1beta2.Tenant, _, newNs *corev1.Namespace) {
				tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{
					NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
						Enforce: &rules.NamespaceRuleEnforceBody{
							Action: rules.ActionTypeDeny,
							Metadata: []rules.MetadataRule{{
								VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"Namespace"}},
								Labels: map[string]rules.MetadataValueRule{podSecurityLabel: {
									Values: []apiruntime.ExpressionMatch{{Exact: []string{"privileged", "baseline"}}},
								}},
							}},
						},
					},
				}}
				newNs.Labels[podSecurityLabel] = "privileged"
			},
			denial: "matched denied rule",
		},
		{
			name: "node selector removal",
			prepare: func(tnt *capsulev1beta2.Tenant, oldNs, newNs *corev1.Namespace) {
				tnt.Spec.NodeSelector = map[string]string{"pool": "oil"}
				oldNs.Annotations = utils.BuildNodeSelector(tnt, oldNs.Annotations)
				delete(newNs.Annotations, utils.NodeSelectorAnnotation)
			},
			denial: "cannot be removed",
		},
		{
			name: "node selector overwrite",
			prepare: func(tnt *capsulev1beta2.Tenant, oldNs, newNs *corev1.Namespace) {
				tnt.Spec.NodeSelector = map[string]string{"pool": "oil"}
				oldNs.Annotations = utils.BuildNodeSelector(tnt, oldNs.Annotations)
				newNs.Annotations[utils.NodeSelectorAnnotation] = "pool=gas"
			},
			denial: "cannot be updated",
		},
		{
			name: "cordoned tenant",
			prepare: func(tnt *capsulev1beta2.Tenant, _, newNs *corev1.Namespace) {
				tnt.Spec.Cordoned = true
				newNs.Labels["example.com/probe"] = "changed"
			},
			denial: "the selected tenant is cordoned",
		},
	}

	for _, subresource := range []string{"", "status", "finalize"} {
		for _, state := range []string{"active", "deleting", "submitted terminating phase", "stored terminating phase"} {
			for _, tt := range tests {
				t.Run("resource="+subresource+"/"+state+"/"+tt.name, func(t *testing.T) {
					tnt, oldNs := namespaceSecurityObjects()
					newNs := oldNs.DeepCopy()
					setNamespaceUpdateState(state, oldNs, newNs)
					tt.prepare(tnt, oldNs, newNs)
					validate, reader := namespaceSecurityAdmission(t, tnt)
					req := namespaceSecurityRequest(t, oldNs, newNs, subresource, "alice")
					assertNamespaceResponse(t, validate(context.Background(), req), tt.denial)
					if reader.gets != 1 || reader.lists != 0 {
						t.Fatalf("direct reads = %d Get, %d List; want one Tenant Get and no Lists", reader.gets, reader.lists)
					}
				})
			}
		}
	}
}

func TestNamespaceAdmissionOutsideTenants(t *testing.T) {
	t.Parallel()

	for _, subresource := range []string{"", "status", "finalize"} {
		for _, state := range []string{"active", "deleting", "submitted terminating phase", "stored terminating phase"} {
			for _, username := range []string{"alice", "administrator", "system:kube-controller-manager"} {
				t.Run("resource="+subresource+"/"+state+"/"+username, func(t *testing.T) {
					oldNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}
					newNs := oldNs.DeepCopy()
					newNs.Labels = map[string]string{podSecurityLabel: "restricted"}
					setNamespaceUpdateState(state, oldNs, newNs)
					validate, reader := namespaceSecurityAdmission(t)
					denial := ""
					if username == "alice" {
						denial = "namespace is not owned by any tenant"
					}
					assertNamespaceResponse(t, validate(context.Background(), namespaceSecurityRequest(t, oldNs, newNs, subresource, username)), denial)
					if reader.gets != 0 || reader.lists != 0 {
						t.Fatalf("unmanaged namespace needed direct reads: %#v", reader)
					}
				})
			}
		}
	}
}

func TestNamespaceCleanupDoesNotResolveTenant(t *testing.T) {
	t.Parallel()

	for _, subresource := range []string{"", "status", "finalize"} {
		t.Run("resource="+subresource, func(t *testing.T) {
			_, oldNs := namespaceSecurityObjects()
			now := metav1.Now()
			oldNs.DeletionTimestamp = &now
			oldNs.Finalizers = []string{"example.com/cleanup"}
			oldNs.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes}
			newNs := oldNs.DeepCopy()
			newNs.Finalizers = nil
			newNs.Spec.Finalizers = nil
			newNs.Status.Phase = corev1.NamespaceTerminating
			newNs.ResourceVersion = "2"
			// Nil dependencies deliberately catch any configuration or Tenant read.
			validate := NamespaceHandler(nil).OnUpdate(nil, nil, admission.NewDecoder(namespaceValidationScheme(t)), nil)
			assertNamespaceResponse(t, validate(context.Background(), namespaceSecurityRequest(t, oldNs, newNs, subresource, "system:kube-controller-manager")), "")
		})
	}
}

func TestNamespaceFinalizeChecksNonTenantOwnerReferences(t *testing.T) {
	t.Parallel()

	tnt, oldNs := namespaceSecurityObjects()
	tnt.Status.Owners[0].Name = "bob"
	newNs := oldNs.DeepCopy()
	newNs.OwnerReferences = append(newNs.OwnerReferences, metav1.OwnerReference{APIVersion: "v1", Kind: "Namespace", Name: "foreign", UID: "foreign-uid"})
	validate, _ := namespaceSecurityAdmission(t, tnt)
	assertNamespaceResponse(t, validate(context.Background(), namespaceSecurityRequest(t, oldNs, newNs, "finalize", "alice")), "denied patch request for this namespace")
}

func namespaceSecurityObjects() (*capsulev1beta2.Tenant, *corev1.Namespace) {
	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "oil", UID: "oil-uid"}}
	tnt.Status.Owners = rbac.OwnerStatusListSpec{{UserSpec: rbac.UserSpec{Name: "alice", Kind: rbac.UserOwner}}}
	ns := namespaceWithTenantReference("oil-prod", tnt.Name, string(tnt.UID))
	ns.Labels[podSecurityLabel] = "restricted"
	ns.Annotations = map[string]string{"example.com/protected": "original"}
	return tnt, ns
}

func setNamespaceUpdateState(state string, oldNs, newNs *corev1.Namespace) {
	switch state {
	case "deleting":
		now := metav1.Now()
		oldNs.DeletionTimestamp, newNs.DeletionTimestamp = &now, &now
	case "submitted terminating phase":
		newNs.Status.Phase = corev1.NamespaceTerminating
	case "stored terminating phase":
		oldNs.Status.Phase, newNs.Status.Phase = corev1.NamespaceTerminating, corev1.NamespaceTerminating
	}
}

func namespaceSecurityRequest(t testing.TB, oldNs, newNs *corev1.Namespace, subresource, username string) admission.Request {
	t.Helper()
	req := namespaceUpdateRequest(t, oldNs, newNs, subresource)
	req.Kind = metav1.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	req.Resource = metav1.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	req.UserInfo = authenticationv1.UserInfo{Username: username, Groups: []string{"system:authenticated"}}
	if username == "alice" {
		req.UserInfo.Groups = append(req.UserInfo.Groups, "projectcapsule.dev")
	}
	return req
}

func namespaceSecurityAdmission(t testing.TB, objects ...client.Object) (handlers.Func, *namespaceCountingReader) {
	t.Helper()
	scheme := namespaceValidationScheme(t)
	users := rbac.UserListSpec{{Name: "projectcapsule.dev", Kind: rbac.GroupOwner}}
	config := &capsulev1beta2.CapsuleConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "capsule"}}
	config.Spec.Users = users
	config.Status.Users = users
	config.Spec.Administrators = rbac.UserListSpec{{Name: "administrator", Kind: rbac.UserOwner}}
	objects = append(objects, config)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	cfg := configuration.NewCapsuleConfiguration(context.Background(), c, c, nil, config.Name)
	reader := &namespaceCountingReader{Reader: c}
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	h := NamespaceHandler(cfg, CordoningHandler(cfg), QuotaHandler(), PrefixHandler(cfg), RulesMetadataHandler(cache.NewRegexCache(), cfg), UserMetadataHandler(), RequiredMetadataHandler())
	decoder := admission.NewDecoder(scheme)
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.OnUpdate(c, reader, decoder, recorder)(ctx, req)
	}, reader
}

type namespaceCountingReader struct {
	client.Reader
	gets, lists int
}

func (r *namespaceCountingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	r.gets++
	return r.Reader.Get(ctx, key, obj, opts...)
}

func (r *namespaceCountingReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	r.lists++
	return r.Reader.List(ctx, list, opts...)
}

func assertNamespaceResponse(t testing.TB, response *admission.Response, denial string) {
	t.Helper()
	if denial == "" {
		if response != nil && !response.Allowed {
			t.Fatalf("unexpected denial: %#v", response.Result)
		}
		return
	}
	if response == nil || response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, denial) {
		t.Fatalf("response = %#v, want denial containing %q", response, denial)
	}
}

func BenchmarkNamespaceUpdateAdmission(b *testing.B) {
	for _, scenario := range []string{"plain metadata", "finalize metadata", "finalize cleanup", "unmanaged status"} {
		b.Run(scenario, func(b *testing.B) {
			tnt, oldNs := namespaceSecurityObjects()
			newNs := oldNs.DeepCopy()
			newNs.Labels["example.com/probe"] = "changed"
			subresource, denial := "", ""
			switch scenario {
			case "finalize metadata":
				subresource = "finalize"
			case "finalize cleanup":
				subresource = "finalize"
				now := metav1.Now()
				oldNs.DeletionTimestamp = &now
				oldNs.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes}
				newNs = oldNs.DeepCopy()
				newNs.Spec.Finalizers = nil
			case "unmanaged status":
				subresource, denial = "status", "namespace is not owned by any tenant"
				oldNs = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}
				newNs = oldNs.DeepCopy()
				newNs.Labels = map[string]string{podSecurityLabel: "restricted"}
			}
			validate, reader := namespaceSecurityAdmission(b, tnt)
			req := namespaceSecurityRequest(b, oldNs, newNs, subresource, "alice")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				assertNamespaceResponse(b, validate(context.Background(), req), denial)
			}
			b.ReportMetric(float64(reader.gets)/float64(b.N), "direct-gets/op")
		})
	}
}
