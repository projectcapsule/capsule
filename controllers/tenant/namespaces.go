// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/valyala/fasttemplate"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/meta"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// Ensuring all annotations are applied to each Namespace handled by the Tenant.
func (r *Manager) reconcileNamespaces(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {
	if err = r.collectNamespaces(ctx, tenant); err != nil {
		err = fmt.Errorf("cannot collect namespaces: %w", err)

		return
	}

	gcSet := make(map[string]struct{})
	for _, inst := range tenant.Status.Spaces {
		gcSet[inst.Name] = struct{}{}
	}

	group := new(errgroup.Group)

	for _, item := range tenant.Status.Namespaces {
		namespace := item

		delete(gcSet, namespace)

		group.Go(func() error {
			return r.reconcileNamespace(ctx, namespace, tenant)
		})
	}

	if err = group.Wait(); err != nil {
		err = fmt.Errorf("cannot sync Namespaces: %w", err)
	}

	for name := range gcSet {
		r.Metrics.DeleteAllMetricsForNamespace(name)

		tenant.Status.RemoveInstance(&capsulev1beta2.TenantStatusNamespaceItem{
			Name: name,
		})
	}

	tenant.Status.Size = uint(len(tenant.Status.Namespaces))

	return
}

func (r *Manager) reconcileNamespace(ctx context.Context, namespace string, tnt *capsulev1beta2.Tenant) (err error) {
	ns := &corev1.Namespace{}
	if err = r.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		return err
	}

	stat := &capsulev1beta2.TenantStatusNamespaceItem{
		Name: namespace,
		UID:  ns.GetUID(),
	}

	metaStatus := &capsulev1beta2.TenantStatusNamespaceMetadata{}

	// Always update tenant status condition after reconciliation
	defer func() {
		instance := tnt.Status.GetInstance(stat)
		if instance != nil {
			stat = instance
		}

		readCondition := meta.NewReadyCondition(ns)

		if err != nil {
			readCondition.Status = metav1.ConditionFalse
			readCondition.Reason = meta.FailedReason
			readCondition.Message = fmt.Sprintf("Failed to reconcile: %v", err)

			if instance != nil && instance.Metadata != nil {
				stat.Metadata = instance.Metadata
			}
		} else if metaStatus != nil {
			stat.Metadata = metaStatus
		}

		stat.Conditions.UpdateConditionByType(readCondition)

		cordonedCondition := meta.NewCordonedCondition(ns)

		if ns.Labels[meta.CordonedLabel] == meta.CordonedLabelTrigger {
			cordonedCondition.Reason = meta.CordonedReason
			cordonedCondition.Message = "namespace is cordoned"
			cordonedCondition.Status = metav1.ConditionTrue
		}

		stat.Conditions.UpdateConditionByType(cordonedCondition)

		tnt.Status.UpdateInstance(stat)

		r.syncNamespaceStatusMetrics(tnt, ns)
	}()

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (conflictErr error) {
		_, conflictErr = controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
			metaStatus, err = r.reconcileMetadata(ctx, ns, tnt, stat)

			return err
		})

		return conflictErr
	})

	return err
}

//nolint:nestif
func (r *Manager) reconcileMetadata(
	ctx context.Context,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
	stat *capsulev1beta2.TenantStatusNamespaceItem,
) (
	managed *capsulev1beta2.TenantStatusNamespaceMetadata,
	err error,
) {
	capsuleLabel, _ := utils.GetTypeLabel(&capsulev1beta2.Tenant{})

	originLabels := ns.GetLabels()
	if originLabels == nil {
		originLabels = make(map[string]string)
	}

	originAnnotations := ns.GetAnnotations()
	if originAnnotations == nil {
		originAnnotations = make(map[string]string)
	}

	managedAnnotations := buildNamespaceAnnotationsForTenant(tnt)
	managedLabels := buildNamespaceLabelsForTenant(tnt)

	if opts := tnt.Spec.NamespaceOptions; opts != nil && len(opts.AdditionalMetadataList) > 0 {
		for _, md := range opts.AdditionalMetadataList {
			var ok bool

			ok, err = utils.IsNamespaceSelectedBySelector(ns, md.NamespaceSelector)
			if err != nil {
				return managed, err
			}

			if !ok {
				continue
			}

			applyTemplateMap(md.Labels, tnt, ns)
			applyTemplateMap(md.Annotations, tnt, ns)

			utils.MapMergeNoOverrite(managedLabels, md.Labels)
			utils.MapMergeNoOverrite(managedAnnotations, md.Annotations)
		}
	}

	managedMetadataOnly := tnt.Spec.NamespaceOptions != nil && tnt.Spec.NamespaceOptions.ManagedMetadataOnly

	// Handle User-Defined Metadata, if allowed
	if !managedMetadataOnly {
		if originLabels != nil {
			maps.Copy(originLabels, managedLabels)
		}

		if originAnnotations != nil {
			maps.Copy(originAnnotations, managedAnnotations)
		}

		// Cleanup old Metadata
		instance := tnt.Status.GetInstance(stat)
		if instance != nil && instance.Metadata != nil {
			for label := range instance.Metadata.Labels {
				if _, ok := managedLabels[label]; ok {
					continue
				}

				delete(originLabels, label)
			}

			for annotation := range instance.Metadata.Annotations {
				if _, ok := managedAnnotations[annotation]; ok {
					continue
				}

				delete(originAnnotations, annotation)
			}
		}

		managed = &capsulev1beta2.TenantStatusNamespaceMetadata{
			Labels:      managedLabels,
			Annotations: managedAnnotations,
		}
	} else {
		originLabels = managedLabels
		originAnnotations = managedAnnotations
	}

	originLabels["kubernetes.io/metadata.name"] = ns.GetName()
	originLabels[capsuleLabel] = tnt.GetName()

	ns.SetLabels(originLabels)
	ns.SetAnnotations(originAnnotations)

	return managed, err
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

	if tnt.Spec.Cordoned {
		labels[meta.CordonedLabel] = "true"
	}

	return labels
}

func (r *Manager) collectNamespaces(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {
	list := &corev1.NamespaceList{}

	err = r.List(ctx, list, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".metadata.ownerReferences[*].capsule", tenant.GetName()),
	})
	if err != nil {
		return
	}

	tenant.AssignNamespaces(list.Items)

	return
}

// applyTemplateMap applies templating to all values in the provided map in place.
func applyTemplateMap(m map[string]string, tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) {
	for k, v := range m {
		if !strings.Contains(v, "{{ ") && !strings.Contains(v, " }}") {
			continue
		}

		t := fasttemplate.New(v, "{{ ", " }}")
		tmplString := t.ExecuteString(map[string]interface{}{
			"tenant.name": tnt.Name,
			"namespace":   ns.Name,
		})

		m[k] = tmplString
	}
}
