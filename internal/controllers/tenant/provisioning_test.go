// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func TestRoleBindingsPrecedeResourceProvisioning(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Spec: capsulev1beta2.TenantSpec{
		Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}},
	}}
	ns := cleanupOwnedNamespace(tnt, "new", false)
	other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
	foreign := cleanupOwnedNamespace(other, "foreign", false)
	r, _ := namespaceCleanupFixture(t, tnt, ns, other, foreign)
	base := r.Client
	checked := false
	r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*networkingv1.NetworkPolicyList); ok {
				checked = true
				status := &capsulev1beta2.RuleStatus{}
				if err := base.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status); err != nil {
					return fmt.Errorf("RuleStatus must precede resource provisioning: %w", err)
				}
				assertBindingSubject(t, ctx, base, ns.Name, "alice", true)
				assertBindingSubject(t, ctx, base, foreign.Name, "alice", false)
			}
			return c.List(ctx, list, opts...)
		},
	})
	if err := r.reconcile(t.Context(), logr.Discard(), tnt.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("resource provisioning was skipped")
	}
}

func TestResourceProvisioningRunsConcurrentlyAndJoinsErrors(t *testing.T) {
	for _, cancelWork := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelWork), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Annotations: map[string]string{
				capsulev1beta2.LimitAnnotationForResource("widgets.example.com_v1"): "10",
			}}
			r, dyn := namespaceCleanupFixture(t, tnt, cleanupOwnedNamespace(tnt, "new", false))
			var finished atomic.Int32
			var recounted, resourceQuotas, globalQuotas atomic.Bool
			dyn.PrependReactor("list", "widgets", func(ktesting.Action) (bool, runtime.Object, error) {
				recounted.Store(finished.Load() == 2)
				return true, &unstructured.UnstructuredList{}, nil
			})
			started := make(chan string, 2)
			release := make(chan struct{})
			networkErr, limitsErr := errors.New("network unavailable"), errors.New("limits unavailable")
			var networkOnce, limitsOnce sync.Once
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					var phaseErr error
					switch list.(type) {
					case *networkingv1.NetworkPolicyList:
						networkOnce.Do(func() { started <- "network" })
						phaseErr = networkErr
					case *corev1.LimitRangeList:
						limitsOnce.Do(func() { started <- "limits" })
						phaseErr = limitsErr
					case *corev1.ResourceQuotaList:
						resourceQuotas.Store(true)
						return c.List(ctx, list, opts...)
					case *capsulev1beta2.GlobalResourceQuotaList:
						globalQuotas.Store(true)
						return c.List(ctx, list, opts...)
					default:
						return c.List(ctx, list, opts...)
					}
					defer finished.Add(1)
					select {
					case <-release:
						if err := ctx.Err(); err != nil {
							return err
						}

						return phaseErr
					case <-ctx.Done():
						return ctx.Err()
					}
				},
			})
			r.reader = r.Client
			done := make(chan error, 1)
			go func() { done <- r.reconcile(ctx, logr.Discard(), tnt.DeepCopy()) }()
			defer func() {
				close(release)
				err := <-done
				if cancelWork {
					if !errors.Is(err, context.Canceled) {
						t.Errorf("cancellation was lost: %v", err)
					}
				} else if !errors.Is(err, networkErr) || !errors.Is(err, limitsErr) {
					t.Errorf("independent phase errors were lost: %v", err)
				}
				if !cancelWork && (!recounted.Load() || !resourceQuotas.Load() || !globalQuotas.Load()) {
					t.Error("independent quota phases were skipped or usage recount preceded resource provisioning")
				}
			}()
			for range 2 {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("resource phases did not start concurrently")
				}
			}
			if cancelWork {
				cancel()
			}
		})
	}
}
