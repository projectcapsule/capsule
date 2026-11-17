// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestNamespaceFailureDoesNotSuppressUnrestrictedAlternatives(t *testing.T) {
	for _, mode := range []string{"missing", "forbidden", "terminating", "empty-labels"} {
		for _, unrestricted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/unrestricted=%t", mode, unrestricted), func(t *testing.T) {
				f := watchFixtureFor(t, 0)
				global := &capsulev1beta2.GlobalTenantResource{}
				if err := f.client.Get(t.Context(), client.ObjectKey{Name: "global"}, global); err != nil {
					t.Fatal(err)
				}
				global.Spec.Triggers[0].NamespaceSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "missing-label", Operator: metav1.LabelSelectorOpDoesNotExist}}}
				if unrestricted {
					alternative := *global.Spec.Triggers[0].DeepCopy()
					alternative.Name, alternative.NamespaceSelector = "unrestricted", nil
					global.Spec.Triggers = append(global.Spec.Triggers, alternative)
				}
				global.Generation++
				if err := f.client.Update(t.Context(), global); err != nil {
					t.Fatal(err)
				}
				if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
					t.Fatal(err)
				}
				f.watcher.client = interceptor.NewClient(f.client, interceptor.Funcs{Get: func(ctx context.Context, inner client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
					if namespace, ok := object.(*corev1.Namespace); ok {
						switch mode {
						case "missing":
							return apierrors.NewNotFound(corev1.Resource("namespaces"), key.Name)
						case "forbidden":
							return apierrors.NewForbidden(corev1.Resource("namespaces"), key.Name, fmt.Errorf("namespace metadata is unavailable"))
						case "terminating":
							namespace.Name, namespace.DeletionTimestamp = key.Name, new(metav1.Now())
							return nil
						case "empty-labels":
							namespace.Name = key.Name
							return nil
						}
					}
					return inner.Get(ctx, key, object, opts...)
				}})
				f.watcher.matchEvent(t.Context(), resourceWatchEvent{resource: configMapsGVR, operation: admissionv1.Create, object: sourceObject("a")})
				targets := f.notifications.take()
				wantGlobal := unrestricted || mode == "empty-labels"
				if targets["local-a"] != 1 || (targets["global"] == 1) != wantGlobal {
					t.Fatalf("namespace availability selected wrong alternatives: %v", targets)
				}
			})
		}
	}
}
