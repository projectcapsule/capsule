// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestRuleStatusReconcileAvoidsRedundantStatusRequests(t *testing.T) {
	t.Parallel()
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "deny", true: "empty"}[empty], func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			instance := &capsulev1beta2.RuleStatus{Name: "rules", Namespace: "team", Generation: 1}
			if !empty {
				instance.Spec = []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny}}}
			}
			gets, updates, patches := 0, 0, 0
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(instance).WithObjects(instance).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						gets++
						return c.Get(ctx, key, obj, opts...)
					},
					SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						updates++
						return c.SubResource(sub).Update(ctx, obj, opts...)
					},
					SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
						patches++
						return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
					},
				}).Build()
			r := &Manager{Client: c, reader: c, Metrics: metrics.NewRuleStatusRecorder(), Log: logr.Discard()}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(instance)}
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if updates != 2 || patches != 0 {
				t.Fatalf("initial status writes: %d updates, %d patches; want Reconciling and Ready updates only", updates, patches)
			}
			gets, updates, patches = 0, 0, 0
			for range 3 {
				if _, err := r.Reconcile(ctx, request); err != nil {
					t.Fatal(err)
				}
			}
			if gets != 3 || updates != 0 || patches != 0 {
				t.Fatalf("steady requests: gets=%d updates=%d patches=%d; want only one cached Get per reconcile", gets, updates, patches)
			}
			if err := c.Get(ctx, request.NamespacedName, instance); err != nil {
				t.Fatal(err)
			}
			if !meta.IsStatusConditionTrue(instance.Status.Conditions, meta.ReadyCondition) || instance.Status.ObservedGeneration != 1 {
				t.Fatalf("unexpected final status: %#v", instance.Status)
			}
			updates = 0
			if err := r.publishRulesStatus(ctx, instance); err != nil {
				t.Fatal(err)
			}
			if updates != 0 {
				t.Fatal("unchanged published rules caused a status write")
			}
		})
	}
}

func TestRuleStatusReconcileReturnsManagedMetadataFailuresForRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	value := "managed"
	instance := &capsulev1beta2.RuleStatus{
		Name: "rules", Namespace: "team", Generation: 1,
		Spec: []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
			Metadata: []rules.MetadataRule{{Labels: map[string]rules.MetadataValueRule{"example.com/managed": {Managed: &value}}}},
		}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(instance).WithObjects(instance).Build()
	r := &Manager{Client: c, reader: c, Metrics: metrics.NewRuleStatusRecorder(), Log: logr.Discard()}
	for range 2 {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(instance)})
		if err == nil || !strings.Contains(err.Error(), "REST config is required") {
			t.Fatalf("reconcile error = %v, want retryable managed metadata error", err)
		}
		if err := c.Get(ctx, client.ObjectKeyFromObject(instance), instance); err != nil {
			t.Fatal(err)
		}
		condition := instance.Status.Conditions.GetConditionByType(meta.ReadyCondition)
		if condition == nil || condition.Status != metav1.ConditionFalse {
			t.Fatalf("failed reconciliation marked ready: %#v", instance.Status)
		}
	}
}

func TestReconcileExcludesQuotaFromRuleStatus(t *testing.T) {
	t.Parallel()

	unnamedQuota := rules.ResourceQuotaRule{
		Hard: corev1.ResourceList{
			corev1.ResourceRequestsCPU: resource.MustParse("1"),
		},
	}
	instance := &capsulev1beta2.RuleStatus{
		Spec: []*rules.NamespaceRuleBodyNamespace{
			{Quota: []rules.ResourceQuotaRule{unnamedQuota}},
			{
				Quota:   []rules.ResourceQuotaRule{unnamedQuota},
				Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny},
			},
		},
	}

	if err := (Manager{}).reconcile(context.Background(), instance); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}
	if len(instance.Status.Rules) != 1 {
		t.Fatalf("status rules = %d, want one enforcement rule", len(instance.Status.Rules))
	}
	if len(instance.Status.Rules[0].Quota) != 0 {
		t.Fatalf("status quota = %#v, want none", instance.Status.Rules[0].Quota)
	}
	if instance.Status.Rules[0].Enforce == nil {
		t.Fatal("status enforcement rule was removed")
	}
}

func TestRemoveQuotaDefinitionsCleansLegacyStatus(t *testing.T) {
	t.Parallel()

	status := &capsulev1beta2.RuleStatusStatus{
		Rule: rules.NamespaceRuleBodyNamespace{Quota: []rules.ResourceQuotaRule{{}}},
		Rules: []*rules.NamespaceRuleBodyNamespace{
			nil,
			{Quota: []rules.ResourceQuotaRule{{}}},
		},
	}

	if changed := removeQuotaDefinitions(status); !changed {
		t.Fatal("legacy quota definitions were not reported as changed")
	}

	if len(status.Rule.Quota) != 0 || len(status.Rules[1].Quota) != 0 {
		t.Fatalf("legacy quota definitions were not removed: %#v", status)
	}
	if changed := removeQuotaDefinitions(status); changed {
		t.Fatal("clean status was reported as changed")
	}
}
