// Copyright 2020-2026 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	jsonpatch "gomodules.xyz/jsonpatch/v2"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/utils"
)

const (
	nsAdmissionPrefix = "e2e-ns-admission"
	nsAdmissionKey    = "e2e.projectcapsule.dev/"
	nsAdmissionPSA    = "pod-security.kubernetes.io/enforce"
)

type nsAdmissionTransport struct {
	name      string
	patchType types.PatchType
}

var nsAdmissionTransports = []nsAdmissionTransport{
	{name: "PUT"},
	{name: "JSON patch", patchType: types.JSONPatchType},
	{name: "merge patch", patchType: types.MergePatchType},
	{name: "strategic merge patch", patchType: types.StrategicMergePatchType},
}

// Apply is exercised separately with field additions/replacements. Omitting an
// unowned field in an apply document is not a request to delete that field.
var nsAdmissionApply = nsAdmissionTransport{name: "server-side apply", patchType: types.ApplyPatchType}

// Use the API endpoint directly so a client-side status helper cannot strip the
// metadata under test. Every write uses a fresh resourceVersion and only retries
// conflicts, never admission denials or failed assertions.
func nsAdmissionWrite(ctx context.Context, cs kubernetes.Interface, old, next *corev1.Namespace, subresource string, transport nsAdmissionTransport, dryRun bool) (*corev1.Namespace, error) {
	restClient := cs.CoreV1().RESTClient()
	request := restClient.Put()
	var body any = next
	if transport.patchType != "" {
		request = restClient.Patch(transport.patchType)
		if transport.patchType == types.JSONPatchType {
			oldJSON, err := json.Marshal(old)
			if err != nil {
				return nil, err
			}
			nextJSON, err := json.Marshal(next)
			if err != nil {
				return nil, err
			}
			operations, err := jsonpatch.CreatePatch(oldJSON, nextJSON)
			if err != nil {
				return nil, err
			}
			operations = append(operations, jsonpatch.Operation{Operation: "replace", Path: "/metadata/resourceVersion", Value: old.ResourceVersion})
			body, err = json.Marshal(operations)
			if err != nil {
				return nil, err
			}
		} else {
			var err error
			body, err = client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{}).Data(next)
			if err != nil {
				return nil, err
			}
			if transport.patchType == types.ApplyPatchType {
				var apply map[string]any
				if err = json.Unmarshal(body.([]byte), &apply); err != nil {
					return nil, err
				}
				apply["apiVersion"], apply["kind"] = "v1", "Namespace"
				apply["metadata"].(map[string]any)["name"] = old.Name
				body = apply
				request = request.Param("fieldManager", nsAdmissionPrefix).Param("force", "true")
			}
		}
	}
	request = request.Resource("namespaces").Name(old.Name)
	if subresource != "" {
		request = request.SubResource(subresource)
	}
	if dryRun {
		request = request.Param("dryRun", metav1.DryRunAll)
	}
	result := &corev1.Namespace{}
	err := request.Body(body).Do(ctx).Into(result)
	return result, err
}

func nsAdmissionAttempt(ctx context.Context, admin, actor kubernetes.Interface, name, subresource string, transport nsAdmissionTransport, change func(*corev1.Namespace), dryRun bool) (old, response *corev1.Namespace, err error) {
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var getErr error
		old, getErr = admin.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		next := old.DeepCopy()
		change(next)
		response, getErr = nsAdmissionWrite(ctx, actor, old, next, subresource, transport, dryRun)
		return getErr
	})
	return
}

func nsAdmissionExpectDenied(ctx context.Context, admin, actor kubernetes.Interface, name, subresource string, transport nsAdmissionTransport, change func(*corev1.Namespace), message string, dryRun bool) {
	GinkgoHelper()
	old, _, err := nsAdmissionAttempt(ctx, admin, actor, name, subresource, transport, change, dryRun)
	nsAdmissionAssertChanged(old, change)
	Expect(err).To(HaveOccurred(), "metadata write must be rejected before persistence")
	Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected admission Forbidden, got %v", err)
	Expect(err).To(MatchError(ContainSubstring("admission webhook \"namespaces.")))
	Expect(err).To(MatchError(ContainSubstring("projectcapsule.dev\" denied the request")))
	if message != "" {
		Expect(err).To(MatchError(ContainSubstring(message)))
	}
	current, getErr := admin.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	Expect(getErr).NotTo(HaveOccurred())
	nsAdmissionExpectMetadata(current, old)
}

func nsAdmissionExpectMetadata(actual, expected *corev1.Namespace) {
	GinkgoHelper()
	Expect(actual.Labels).To(Equal(expected.Labels))
	Expect(actual.Annotations).To(Equal(expected.Annotations))
	Expect(actual.OwnerReferences).To(Equal(expected.OwnerReferences))
}

func nsAdmissionGrant(ctx context.Context, name string, subjects []rbacv1.Subject) {
	GinkgoHelper()
	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}, Rules: []rbacv1.PolicyRule{{
		APIGroups: []string{""}, Resources: []string{"namespaces", "namespaces/status", "namespaces/finalize"}, Verbs: []string{"get", "update", "patch"},
	}}}
	Expect(k8sClient.Create(ctx, role)).To(Succeed())
	DeferCleanup(EventuallyDeletion, role)
	binding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}, Subjects: subjects, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role.Name}}
	Expect(k8sClient.Create(ctx, binding)).To(Succeed())
	DeferCleanup(EventuallyDeletion, binding)
}

func nsAdmissionCheckRBAC(ctx context.Context, cs kubernetes.Interface, name string) {
	GinkgoHelper()
	for _, subresource := range []string{"", "status", "finalize"} {
		for _, verb := range []string{"get", "update", "patch"} {
			Eventually(func(g Gomega) {
				review, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{Group: "", Resource: "namespaces", Subresource: subresource, Verb: verb, Name: name},
				}}, metav1.CreateOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(review.Status.Allowed).To(BeTrue(), "%s namespaces/%s must be granted independently of admission: %+v", verb, subresource, review.Status)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	}
}

func nsAdmissionTenant(name string, owners rbac.OwnerListSpec) *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: owners}}
}

// Keep keys independent so each case must reach the intended policy handler.
//
//nolint:staticcheck
func nsAdmissionPolicies(tnt *capsulev1beta2.Tenant) {
	tnt.Spec.NodeSelector = map[string]string{nsAdmissionKey + "pool": "isolated"}
	tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
		ForbiddenLabels:      api.ForbiddenListSpec{Exact: []string{nsAdmissionPSA, nsAdmissionKey + "forbidden", nsAdmissionKey + "forbidden-new"}, Regex: "^e2e\\.projectcapsule\\.dev/regex-"},
		ForbiddenAnnotations: api.ForbiddenListSpec{Exact: []string{nsAdmissionKey + "forbidden", nsAdmissionKey + "forbidden-new"}, Regex: "^e2e\\.projectcapsule\\.dev/regex-"},
		RequiredMetadata: &capsulev1beta2.RequiredMetadata{
			Labels: map[string]string{nsAdmissionKey + "required": "^locked$"}, Annotations: map[string]string{nsAdmissionKey + "required": "^locked$"},
		},
	}
	deny := rules.MetadataValueRule{Values: []apiruntime.ExpressionMatch{{Exact: []string{"denied"}}}}
	required := rules.MetadataValueRule{Required: true, Values: []apiruntime.ExpressionMatch{{Exact: []string{"locked"}}}}
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{
		nsAdmissionMetadataRule(rules.ActionTypeDeny, map[string]rules.MetadataValueRule{nsAdmissionKey + "rule-deny": deny, nsAdmissionKey + "rule-deny-new": deny}),
		nsAdmissionMetadataRule(rules.ActionTypeAllow, map[string]rules.MetadataValueRule{nsAdmissionKey + "rule-required": required}),
	}
	psa := nsAdmissionMetadataRule(rules.ActionTypeDeny, map[string]rules.MetadataValueRule{nsAdmissionPSA: {Values: []apiruntime.ExpressionMatch{{Exact: []string{"privileged", "baseline"}}}}})
	psa.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{nsAdmissionKey + "policy": "rules"}}
	tnt.Spec.Rules = append(tnt.Spec.Rules, psa)
}

func nsAdmissionMetadataRule(action rules.ActionType, metadata map[string]rules.MetadataValueRule) *rules.NamespaceRuleBodyTenant {
	return &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action: action, Metadata: []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"Namespace"}}, Labels: metadata, Annotations: maps.Clone(metadata)}},
	}}}
}

func nsAdmissionNamespace(ctx context.Context, admin kubernetes.Interface, name string, tnt *capsulev1beta2.Tenant, policy string) *corev1.Namespace {
	GinkgoHelper()
	ns := NewNamespace(name)
	ns.Annotations = map[string]string{}
	if tnt != nil {
		ns.Labels[meta.TenantLabel] = tnt.Name
		if tnt.Spec.NamespaceOptions != nil {
			for _, field := range []map[string]string{ns.Labels, ns.Annotations} {
				for _, key := range []string{"forbidden", "regex-existing", "required", "rule-required", "rule-deny", "allowed"} {
					field[nsAdmissionKey+key] = "locked"
				}
			}
			ns.Labels[nsAdmissionPSA] = "restricted"
			ns.Labels[nsAdmissionKey+"policy"] = policy
			ns.Annotations = utils.BuildNodeSelector(tnt, ns.Annotations)
		}
	}
	var created *corev1.Namespace
	Eventually(func() error {
		var err error
		created, err = admin.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		return err
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	DeferCleanup(ForceDeleteNamespace, ctx, name)
	if tnt != nil {
		NamespaceIsPartOfTenant(tnt, created).Should(Succeed())
	}
	return created
}

func nsAdmissionMetadataChange(field, key, operation, value string) func(*corev1.Namespace) {
	return func(ns *corev1.Namespace) {
		metadata := ns.Labels
		if field == "annotations" {
			metadata = ns.Annotations
		}
		if metadata == nil {
			metadata = map[string]string{}
			if field == "annotations" {
				ns.Annotations = metadata
			} else {
				ns.Labels = metadata
			}
		}
		if operation == "remove" {
			delete(metadata, key)
		} else {
			metadata[key] = value
		}
	}
}

func nsAdmissionTenantReference(ns *corev1.Namespace) *metav1.OwnerReference {
	for i := range ns.OwnerReferences {
		if ns.OwnerReferences[i].Kind == "Tenant" && strings.HasPrefix(ns.OwnerReferences[i].APIVersion, capsulev1beta2.GroupVersion.Group+"/") {
			return &ns.OwnerReferences[i]
		}
	}
	Fail("fixture must have a Tenant ownerReference")
	return nil
}

func nsAdmissionExpectAllowed(ctx context.Context, admin, actor kubernetes.Interface, name, subresource string, transport nsAdmissionTransport, change func(*corev1.Namespace), dryRun bool) {
	GinkgoHelper()
	old, response, err := nsAdmissionAttempt(ctx, admin, actor, name, subresource, transport, change, dryRun)
	Expect(err).NotTo(HaveOccurred())
	expected := old.DeepCopy()
	change(expected)
	nsAdmissionExpectMetadata(response, expected)
	current, err := admin.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	if dryRun {
		nsAdmissionExpectMetadata(current, old)
	} else {
		nsAdmissionExpectMetadata(current, expected)
	}
	// Restore only the fields changed by this test; unrelated controller metadata
	// must not be overwritten by cleanup. Use the actual admission response too,
	// so a controller undoing an accepted request cannot make a test pass.
	if !dryRun {
		DeferCleanup(func() {
			_, _, err := nsAdmissionAttempt(ctx, admin, admin, name, "", nsAdmissionTransports[2], func(ns *corev1.Namespace) {
				for _, pair := range []struct{ before, after, current map[string]string }{{old.Labels, expected.Labels, ns.Labels}, {old.Annotations, expected.Annotations, ns.Annotations}} {
					for key := range pair.after {
						if pair.before[key] != pair.after[key] {
							if value, ok := pair.before[key]; ok {
								pair.current[key] = value
							} else {
								delete(pair.current, key)
							}
						}
					}
					for key, value := range pair.before {
						if _, ok := pair.after[key]; !ok {
							pair.current[key] = value
						}
					}
				}
			}, false)
			Expect(err).NotTo(HaveOccurred())
		})
	}
}

func nsAdmissionAssertChanged(old *corev1.Namespace, change func(*corev1.Namespace)) {
	GinkgoHelper()
	next := old.DeepCopy()
	change(next)
	Expect(reflect.DeepEqual(old.ObjectMeta, next.ObjectMeta)).To(BeFalse(), fmt.Sprintf("test must change metadata on %s", old.Name))
}
