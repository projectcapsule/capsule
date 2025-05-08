// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// Ensuring all annotations are applied to each Namespace handled by the Tenant.
func (r *Manager) syncNamespaces(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {
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

func (r *Manager) syncNamespaceMetadata(ctx context.Context, namespace string, tnt *capsulev1beta2.Tenant) (err error) {
	var res controllerutil.OperationResult

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (conflictErr error) {
		ns := &corev1.Namespace{}
		if conflictErr = r.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
			return conflictErr
		}

		capsuleLabel, _ := utils.GetTypeLabel(&capsulev1beta2.Tenant{})

		res, conflictErr = controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
			annotations := buildNamespaceAnnotationsForTenant(tnt)
			labels := buildNamespaceLabelsForTenant(tnt)

			if opts := tnt.Spec.NamespaceOptions; opts != nil && len(opts.AdditionalMetadataList) > 0 {
				for _, md := range opts.AdditionalMetadataList {
					ok, err := utils.IsNamespaceSelectedBySelector(ns, md.NamespaceSelector)
					if err != nil {
						return err
					}

					if !ok {
						continue
					}

					maps.Copy(labels, md.Labels)
					maps.Copy(annotations, md.Annotations)
				}
			}

			labels["kubernetes.io/metadata.name"] = namespace
			labels[capsuleLabel] = tnt.GetName()

			if tnt.Spec.Cordoned {
				ns.Labels[utils.CordonedLabel] = "true"
			} else {
				delete(ns.Labels, utils.CordonedLabel)
			}

			if ns.Annotations == nil {
				ns.SetAnnotations(annotations)
			} else {
				maps.Copy(ns.Annotations, annotations)
			}

			if ns.Labels == nil {
				ns.SetLabels(labels)
			} else {
				maps.Copy(ns.Labels, labels)
			}

			return nil
		})

		return conflictErr
	})

	r.emitEvent(tnt, namespace, res, "Ensuring Namespace metadata", err)

	return err
}

func buildNamespaceAnnotationsForTenant(tnt *capsulev1beta2.Tenant) map[string]string {
	annotations := make(map[string]string)

	if md := tnt.Spec.NamespaceOptions; md != nil && md.AdditionalMetadata != nil {
		maps.Copy(annotations, md.AdditionalMetadata.Annotations)
	}

	if tnt.Spec.NodeSelector != nil {
		annotations = utils.BuildNodeSelector(tnt, annotations)
	}

	if ic := tnt.Spec.IngressOptions.AllowedClasses; ic != nil {
		if len(ic.Exact) > 0 {
			annotations[AvailableIngressClassesAnnotation] = strings.Join(ic.Exact, ",")
		}

		if len(ic.Regex) > 0 {
			annotations[AvailableIngressClassesRegexpAnnotation] = ic.Regex
		}
	}

	if sc := tnt.Spec.StorageClasses; sc != nil {
		if len(sc.Exact) > 0 {
			annotations[AvailableStorageClassesAnnotation] = strings.Join(sc.Exact, ",")
		}

		if len(sc.Regex) > 0 {
			annotations[AvailableStorageClassesRegexpAnnotation] = sc.Regex
		}
	}

	if cr := tnt.Spec.ContainerRegistries; cr != nil {
		if len(cr.Exact) > 0 {
			annotations[AllowedRegistriesAnnotation] = strings.Join(cr.Exact, ",")
		}

		if len(cr.Regex) > 0 {
			annotations[AllowedRegistriesRegexpAnnotation] = cr.Regex
		}
	}

	for _, key := range []string{
		api.ForbiddenNamespaceLabelsAnnotation,
		api.ForbiddenNamespaceLabelsRegexpAnnotation,
		api.ForbiddenNamespaceAnnotationsAnnotation,
		api.ForbiddenNamespaceAnnotationsRegexpAnnotation,
	} {
		if value, ok := tnt.Annotations[key]; ok {
			annotations[key] = value
		}
	}

	return annotations
}

func buildNamespaceLabelsForTenant(tnt *capsulev1beta2.Tenant) map[string]string {
	labels := make(map[string]string)

	if md := tnt.Spec.NamespaceOptions; md != nil && md.AdditionalMetadata != nil {
		maps.Copy(labels, md.AdditionalMetadata.Labels)
	}

	return labels
}

func (r *Manager) ensureNamespaceCount(ctx context.Context, tenant *capsulev1beta2.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		tenant.Status.Size = uint(len(tenant.Status.Namespaces))

		found := &capsulev1beta2.Tenant{}
		if err := r.Get(ctx, types.NamespacedName{Name: tenant.GetName()}, found); err != nil {
			return err
		}

		found.Status.Size = tenant.Status.Size

		return r.Client.Status().Update(ctx, found, &client.SubResourceUpdateOptions{})
	})
}

func (r *Manager) collectNamespaces(ctx context.Context, tenant *capsulev1beta2.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		list := &corev1.NamespaceList{}

		err = r.List(ctx, list, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".metadata.ownerReferences[*].capsule", tenant.GetName()),
		})
		if err != nil {
			return
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, tenant.DeepCopy(), func() error {
			tenant.AssignNamespaces(list.Items)

			return r.Client.Status().Update(ctx, tenant, &client.SubResourceUpdateOptions{})
		})

		return
	})
}
