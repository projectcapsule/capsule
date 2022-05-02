// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

// Ensuring all annotations are applied to each Namespace handled by the Tenant.
func (r *Manager) syncNamespaces(ctx context.Context, tenant *capsulev1beta1.Tenant) (err error) {
	group := new(errgroup.Group)

	for _, item := range tenant.Status.Namespaces {
		namespace := item

		group.Go(func() error {
			return r.syncNamespaceMetadata(ctx, namespace, tenant)
		})
	}

	if err = group.Wait(); err != nil {
		r.Log.Error(err, "Cannot sync Namespaces")

		err = fmt.Errorf("cannot sync Namespaces: %w", err)
	}

	return
}

// nolint:gocognit
func (r *Manager) syncNamespaceMetadata(ctx context.Context, namespace string, tnt *capsulev1beta1.Tenant) (err error) {
	var res controllerutil.OperationResult

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (conflictErr error) {
		ns := &corev1.Namespace{}
		if conflictErr = r.Client.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
			return
		}

		capsuleLabel, _ := capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{})

		res, conflictErr = controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
			annotations := make(map[string]string)
			labels := map[string]string{
				"name":       namespace,
				capsuleLabel: tnt.GetName(),
			}

			if tnt.Spec.NamespaceOptions != nil && tnt.Spec.NamespaceOptions.AdditionalMetadata != nil {
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadata.Annotations {
					annotations[k] = v
				}
			}

			if tnt.Spec.NamespaceOptions != nil && tnt.Spec.NamespaceOptions.AdditionalMetadata != nil {
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadata.Labels {
					labels[k] = v
				}
			}

			if tnt.Spec.NodeSelector != nil {
				var selector []string
				for k, v := range tnt.Spec.NodeSelector {
					selector = append(selector, fmt.Sprintf("%s=%s", k, v))
				}
				annotations["scheduler.alpha.kubernetes.io/node-selector"] = strings.Join(selector, ",")
			}

			if tnt.Spec.IngressOptions.AllowedClasses != nil {
				if len(tnt.Spec.IngressOptions.AllowedClasses.Exact) > 0 {
					annotations[capsulev1beta1.AvailableIngressClassesAnnotation] = strings.Join(tnt.Spec.IngressOptions.AllowedClasses.Exact, ",")
				}
				if len(tnt.Spec.IngressOptions.AllowedClasses.Regex) > 0 {
					annotations[capsulev1beta1.AvailableIngressClassesRegexpAnnotation] = tnt.Spec.IngressOptions.AllowedClasses.Regex
				}
			}

			if tnt.Spec.StorageClasses != nil {
				if len(tnt.Spec.StorageClasses.Exact) > 0 {
					annotations[capsulev1beta1.AvailableStorageClassesAnnotation] = strings.Join(tnt.Spec.StorageClasses.Exact, ",")
				}
				if len(tnt.Spec.StorageClasses.Regex) > 0 {
					annotations[capsulev1beta1.AvailableStorageClassesRegexpAnnotation] = tnt.Spec.StorageClasses.Regex
				}
			}

			if tnt.Spec.ContainerRegistries != nil {
				if len(tnt.Spec.ContainerRegistries.Exact) > 0 {
					annotations[capsulev1beta1.AllowedRegistriesAnnotation] = strings.Join(tnt.Spec.ContainerRegistries.Exact, ",")
				}
				if len(tnt.Spec.ContainerRegistries.Regex) > 0 {
					annotations[capsulev1beta1.AllowedRegistriesRegexpAnnotation] = tnt.Spec.ContainerRegistries.Regex
				}
			}

			if value, ok := tnt.Annotations[capsulev1beta1.ForbiddenNamespaceLabelsAnnotation]; ok {
				annotations[capsulev1beta1.ForbiddenNamespaceLabelsAnnotation] = value
			}

			if value, ok := tnt.Annotations[capsulev1beta1.ForbiddenNamespaceLabelsRegexpAnnotation]; ok {
				annotations[capsulev1beta1.ForbiddenNamespaceLabelsRegexpAnnotation] = value
			}

			if value, ok := tnt.Annotations[capsulev1beta1.ForbiddenNamespaceAnnotationsAnnotation]; ok {
				annotations[capsulev1beta1.ForbiddenNamespaceAnnotationsAnnotation] = value
			}

			if value, ok := tnt.Annotations[capsulev1beta1.ForbiddenNamespaceAnnotationsRegexpAnnotation]; ok {
				annotations[capsulev1beta1.ForbiddenNamespaceAnnotationsRegexpAnnotation] = value
			}

			if ns.Annotations == nil {
				ns.SetAnnotations(annotations)
			} else {
				for k, v := range annotations {
					ns.Annotations[k] = v
				}
			}

			if ns.Labels == nil {
				ns.SetLabels(labels)
			} else {
				for k, v := range labels {
					ns.Labels[k] = v
				}
			}

			return nil
		})

		return
	})

	r.emitEvent(tnt, namespace, res, "Ensuring Namespace metadata", err)

	return err
}

func (r *Manager) ensureNamespaceCount(ctx context.Context, tenant *capsulev1beta1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		tenant.Status.Size = uint(len(tenant.Status.Namespaces))

		found := &capsulev1beta1.Tenant{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: tenant.GetName()}, found); err != nil {
			return err
		}

		found.Status.Size = tenant.Status.Size

		return r.Client.Status().Update(ctx, found, &client.UpdateOptions{})
	})
}

func (r *Manager) collectNamespaces(ctx context.Context, tenant *capsulev1beta1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		list := &corev1.NamespaceList{}
		err = r.Client.List(ctx, list, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".metadata.ownerReferences[*].capsule", tenant.GetName()),
		})

		if err != nil {
			return
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, tenant.DeepCopy(), func() error {
			tenant.AssignNamespaces(list.Items)

			return r.Client.Status().Update(ctx, tenant, &client.UpdateOptions{})
		})

		return
	})
}
