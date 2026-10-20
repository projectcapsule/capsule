// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

// Namespace events also reach this queue directly. They need not wait for the
// Tenant's quota, usage, class discovery or status phases to release its key.
func (r *Manager) setupNamespaceProvisioningController(mgr ctrl.Manager, config utils.ControllerOptions) error {
	reader, err := isolatedNamespaceReader(mgr)
	if err != nil {
		return fmt.Errorf("create namespace provisioning reader: %w", err)
	}

	r.provisioningReader = reader

	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/tenants/namespace-provisioning").
		For(&corev1.Namespace{}, builder.WithPredicates(predicates.NamespaceTenantStateChangedPredicate{}, predicate.NewPredicateFuncs(func(obj client.Object) bool {
			ns, ok := obj.(*corev1.Namespace)

			return ok && ns.DeletionTimestamp == nil && len(tenant.TenantOwnerReferences(ns)) == 1
		}))).
		WithOptions(config.Runtime.ToControllerOptions()).
		Complete(reconcile.Func(r.reconcileNamespaceProvisioning))
}

func (r *Manager) reconcileNamespaceProvisioning(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var result reconcile.Result

	acquired, err := r.synchronizeNamespaceProvisioning(ctx, request.Name, false, func() error {
		reader := r.namespaceProvisioningReader()

		ns := &corev1.Namespace{}

		if err := reader.Get(ctx, request.NamespacedName, ns); err != nil {
			return client.IgnoreNotFound(err)
		}

		refs := tenant.TenantOwnerReferences(ns)
		if ns.UID == "" || ns.DeletionTimestamp != nil || len(refs) != 1 || refs[0].UID == "" || refs[0].Name == "" {
			return nil
		}

		tnt := &capsulev1beta2.Tenant{}
		if err := reader.Get(ctx, client.ObjectKey{Name: refs[0].Name}, tnt); err != nil {
			return client.IgnoreNotFound(err)
		}

		if tnt.DeletionTimestamp != nil || !namespaceOwnedByTenant(ns, tnt) {
			return nil
		}
		// Initial owner/promotion evaluation remains the Tenant controller's job.
		// Never turn an unobserved policy revision into grants from older status.
		if tnt.Status.ObservedGeneration != tnt.Generation {
			result.RequeueAfter = 250 * time.Millisecond

			return nil
		}

		// A published RuleStatus alone does not prove that RBAC succeeded. Keep
		// retrying partial provisioning until the Tenant records this UID as ready.
		// Established profiles remain on the Tenant queue, where promotion rules
		// are evaluated across the complete membership before permission updates.
		if status := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name, UID: ns.UID}); status != nil && status.UID == ns.UID {
			if ready := status.Conditions.GetConditionByType(meta.ReadyCondition); ready != nil && ready.Status == metav1.ConditionTrue {
				return nil
			}
		}

		plan := prepareNamespaceProvisioningRoleBindings(tnt)
		bindings, bindingErr := plan.forNamespace(ns)
		log := ctrl.LoggerFrom(ctx)

		// Only the Tenant queue records managed metadata in status.spaces.
		// Leave Namespace writes there so no cleanup inventory can be lost.
		if err := r.reconcileRuleStatus(ctx, log, tnt, tnt, ns); err != nil {
			return err
		}

		if bindingErr != nil {
			return bindingErr
		}

		return r.runPhase(log, "namespace_role_bindings", func() error {
			return r.syncAdditionalRoleBinding(ctx, log, tnt, ns.Name, bindings, false)
		})
	})

	if !acquired && err == nil {
		result.RequeueAfter = 100 * time.Millisecond
	}

	return result, err
}

type namespaceProvisioningSync struct {
	token chan struct{}
	refs  int
}

// Keep only active keys, including waiters. Acquiring a busy namespace can be
// cancelled; independent namespaces never hold one another's lock across I/O.
func (r *Manager) withNamespaceProvisioning(ctx context.Context, name string, fn func() error) error {
	_, err := r.synchronizeNamespaceProvisioning(ctx, name, true, fn)

	return err
}

func (r *Manager) synchronizeNamespaceProvisioning(ctx context.Context, name string, wait bool, fn func() error) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	r.namespaceProvisioningMu.Lock()
	if r.namespaceProvisioning == nil {
		r.namespaceProvisioning = make(map[string]*namespaceProvisioningSync)
	}

	entry := r.namespaceProvisioning[name]
	if entry == nil {
		entry = &namespaceProvisioningSync{token: make(chan struct{}, 1)}
		r.namespaceProvisioning[name] = entry
	}

	entry.refs++

	r.namespaceProvisioningMu.Unlock()

	defer func() {
		r.namespaceProvisioningMu.Lock()
		defer r.namespaceProvisioningMu.Unlock()

		entry.refs--
		if entry.refs == 0 {
			delete(r.namespaceProvisioning, name)
		}
	}()

	if wait {
		select {
		case entry.token <- struct{}{}:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	} else {
		select {
		case entry.token <- struct{}{}:
		default:
			return false, nil
		}
	}

	defer func() { <-entry.token }()

	if err := ctx.Err(); err != nil {
		return true, err
	}

	return true, fn()
}

func (r *Manager) namespaceProvisioningReader() client.Reader {
	if r.provisioningReader != nil {
		return r.provisioningReader
	}

	if r.reader != nil {
		return r.reader
	}

	return r.Client
}

func (r *Manager) provisionNamespace(
	ctx context.Context,
	log logr.Logger,
	expected *corev1.Namespace,
	snapshot *capsulev1beta2.Tenant,
	plan *namespaceRoleBindingPlan,
) (stat *capsulev1beta2.TenantStatusNamespaceItem, err error) {
	err = r.withNamespaceProvisioning(ctx, expected.Name, func() error {
		// Both queues share this writer. Read the namespace after acquiring
		// its key so rule selection sees metadata written by the other queue.
		ns := &corev1.Namespace{}
		if err := r.namespaceProvisioningReader().Get(ctx, client.ObjectKeyFromObject(expected), ns); err != nil {
			return client.IgnoreNotFound(err)
		}

		if ns.UID != expected.UID || !namespaceOwnedByTenant(ns, snapshot) {
			return nil
		}

		stat, err = r.provisionNamespaceLocked(ctx, log, ns, snapshot, plan)

		return err
	})

	if err != nil && stat == nil {
		// A transient read or lock error is not evidence that this namespace
		// disappeared. Retain its cleanup inventory until a successful retry;
		// never carry inventory across namespace recreation.
		stat = &capsulev1beta2.TenantStatusNamespaceItem{Name: expected.Name, UID: expected.UID}
		if previous := snapshot.Status.GetInstance(stat); previous != nil && previous.UID == expected.UID {
			stat = previous.DeepCopy()
		}

		ready := meta.NewReadyCondition(expected)
		ready.Status, ready.Reason, ready.Message = metav1.ConditionFalse, meta.FailedReason, fmt.Sprintf("Failed to provision namespace: %v", err)
		stat.Conditions.UpdateConditionByType(ready)
	}

	return stat, err
}

// Tenant workers hold the namespace key and pass an immutable evaluated Tenant.
func (r *Manager) provisionNamespaceLocked(ctx context.Context, log logr.Logger, ns *corev1.Namespace, snapshot *capsulev1beta2.Tenant, plan *namespaceRoleBindingPlan) (*capsulev1beta2.TenantStatusNamespaceItem, error) {
	// Select permissions from exactly the namespace snapshot used to render
	// enforcement, before metadata reconciliation changes its labels.
	bindings, bindingErr := plan.forNamespace(ns)

	stat, profileErr := r.reconcileNamespace(ctx, log, ns, snapshot, snapshot)
	if profileErr != nil || ns.DeletionTimestamp != nil {
		return stat, profileErr
	}

	if bindingErr == nil {
		bindingErr = r.runPhase(log, "namespace_role_bindings", func() error {
			return r.syncAdditionalRoleBinding(ctx, log, snapshot, ns.Name, bindings, plan.prune)
		})
	}

	if bindingErr != nil {
		ready := meta.NewReadyCondition(ns)
		ready.Status, ready.Reason, ready.Message = metav1.ConditionFalse, meta.PermissionsFailedReason, fmt.Sprintf("Failed to reconcile namespace permissions: %v", bindingErr)
		stat.Conditions.UpdateConditionByType(ready)
	}

	return stat, bindingErr
}
