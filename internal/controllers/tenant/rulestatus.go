// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func (r *Manager) reconcileRuleStatus(
	ctx context.Context,
	log logr.Logger,
	tnt *capsulev1beta2.Tenant,
	templateTenant *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
) error {
	// Collect Rules for namespace
	ruleBody, err := tenant.BuildNamespaceRuleBodyStatus(r.Scheme(), ns, templateTenant)
	if err != nil {
		return err
	}

	return r.ensureRuleStatus(
		ctx,
		log,
		tnt,
		ns,
		ruleBody,
	)
}

func (r *Manager) ensureRuleStatus(
	ctx context.Context,
	log logr.Logger,
	tnt *capsulev1beta2.Tenant,
	namespace *corev1.Namespace,
	body []*rules.NamespaceRuleBodyNamespace,
) error {
	rule := &capsulev1beta2.RuleStatus{
		Name:      meta.NameForManagedRuleStatus(),
		Namespace: namespace.GetName(),
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rule, func() error {
		labels := rule.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}

		labels[meta.NewManagedByCapsuleLabel] = meta.ValueController
		labels[meta.NewTenantLabel] = tnt.Name
		labels[meta.CapsuleNameLabel] = rule.GetName()

		rule.SetLabels(labels)

		if body != nil {
			rule.Spec = body
		}

		if err := controllerutil.SetControllerReference(tnt, rule, r.Scheme()); err != nil {
			return err
		}

		// Record the desired projection before writing: the watch event can be
		// delivered before CreateOrUpdate returns. A failed write cannot hide
		// drift, because only an object equal to the desired state is ignored.
		if r.ruleStatusWrites != nil {
			fingerprint, err := ruleStatusProjectionFingerprint(rule)
			if err != nil {
				return err
			}

			r.ruleStatusWrites.Add(client.ObjectKeyFromObject(rule), fingerprint)
		}

		return nil
	})
	if err != nil {
		if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
			log.V(4).Info(
				"skipping RuleStatus sync because namespace is terminating",
				"name", rule.Name,
				"namespace", rule.Namespace,
				"tenant", tnt.Name,
			)

			return nil
		}

		return err
	}

	return nil
}

// Only suppress events matching a projection this controller has produced.
// Cache misses (including after restart/eviction), drift and deletion retain
// their normal repair behavior. Status and resourceVersion are not inputs.
func (r *Manager) ruleStatusChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return !r.expectedRuleStatus(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !predicates.ClassChanged().Update(e) &&
				reflect.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences()) {
				return false
			}

			return !r.expectedRuleStatus(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			if r.ruleStatusWrites != nil {
				r.ruleStatusWrites.Remove(client.ObjectKeyFromObject(e.Object))
			}

			return true
		},
	}
}

func (r *Manager) expectedRuleStatus(obj client.Object) bool {
	rule, ok := obj.(*capsulev1beta2.RuleStatus)
	if !ok || r.ruleStatusWrites == nil || !rule.DeletionTimestamp.IsZero() {
		return false
	}

	expected, ok := r.ruleStatusWrites.Get(client.ObjectKeyFromObject(rule))
	if !ok {
		return false
	}

	fingerprint, err := ruleStatusProjectionFingerprint(rule)

	return err == nil && expected == fingerprint
}

func ruleStatusProjectionFingerprint(rule *capsulev1beta2.RuleStatus) ([sha256.Size]byte, error) {
	raw, err := json.Marshal(struct {
		Spec        []*rules.NamespaceRuleBodyNamespace `json:"spec"`
		Labels      map[string]string                   `json:"labels,omitempty"`
		Annotations map[string]string                   `json:"annotations,omitempty"`
		Owners      []metav1.OwnerReference             `json:"owners,omitempty"`
	}{rule.Spec, rule.Labels, rule.Annotations, rule.OwnerReferences})
	if err != nil {
		return [sha256.Size]byte{}, err
	}

	return sha256.Sum256(raw), nil
}
