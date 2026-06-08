// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func (r *Manager) reconcileRuleStatus(
	ctx context.Context,
	log logr.Logger,
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
) error {
	// Collect Rules for namespace
	ruleBody, err := tenant.BuildNamespaceRuleBodyStatus(ctx, r.Client, ns, tnt)
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      meta.NameForManagedRuleStatus(),
			Namespace: namespace.GetName(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rule, func() error {
		labels := rule.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}

		labels[meta.NewManagedByCapsuleLabel] = meta.ValueController
		labels[meta.CapsuleNameLabel] = rule.GetName()

		rule.SetLabels(labels)

		if body != nil {
			rule.Spec = body
		}

		return controllerutil.SetControllerReference(tnt, rule, r.Scheme())
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
