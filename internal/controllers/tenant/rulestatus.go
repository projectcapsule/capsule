// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func (r *Manager) reconcileRuleStatus(
	ctx context.Context,
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
		tnt,
		ns,
		ruleBody,
	)
}

func (r *Manager) ensureRuleStatus(
	ctx context.Context,
	tnt *capsulev1beta2.Tenant,
	namespace *corev1.Namespace,
	body *api.NamespaceRuleBodyNamespace,
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
			rule.Spec = []*api.NamespaceRuleBodyNamespace{body}
		}

		return controllerutil.SetControllerReference(tnt, rule, r.Scheme())
	})

	return err
}
