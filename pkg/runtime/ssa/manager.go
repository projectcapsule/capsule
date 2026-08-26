// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package ssa manages the server-side apply lifecycle of rendered resources.
package ssa

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

// Metadata configures the labels used to track resources managed by a Manager.
type Metadata struct {
	CreatedByValue     string
	ManagedByValue     string
	ProtectedByValue   string
	LegacyCreatedLabel string
}

// Manager applies and prunes resources with server-side apply.
type Manager struct {
	Reader   client.Reader
	Mapper   k8smeta.RESTMapper
	Metadata Metadata
}

// ApplyOptions configures one server-side apply operation.
type ApplyOptions struct {
	FieldOwner        string
	Force             bool
	Adopt             bool
	Protect           bool
	OwnerReference    *metav1.OwnerReference
	PreviouslyCreated bool
}

// ApplyResult describes the resource after a successful apply.
type ApplyResult struct {
	LastApply *metav1.Time
	Created   bool
}

// PruneOptions configures one server-side apply prune operation.
type PruneOptions struct {
	FieldOwner        string
	PreviouslyCreated bool
	OwnerReference    *metav1.OwnerReference
}

// Apply creates a resource or acquires ownership of the desired fields on an
// existing resource. Existing resources must explicitly allow adoption.
func (m Manager) Apply(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	opts ApplyOptions,
) (ApplyResult, error) {
	desired, err := m.scopedObject(obj)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("resolving object scope: %w", err)
	}

	// Tracking metadata belongs to the lifecycle manager, not to the rendered
	// field manager. This also prevents templates from marking adopted objects
	// as created by Capsule.
	meta.SetFilteredLabels(desired, map[string]struct{}{
		meta.CreatedByCapsuleLabel:    {},
		meta.NewManagedByCapsuleLabel: {},
		meta.ProtectedByCapsuleLabel:  {},
	})

	if opts.Protect && m.Metadata.ProtectedByValue != "" {
		labels := desired.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		labels[meta.ProtectedByCapsuleLabel] = m.Metadata.ProtectedByValue
		desired.SetLabels(labels)
	}

	actual := objectReference(desired)
	key := client.ObjectKeyFromObject(actual)

	patches, created, err := m.managedMetadataPatches(ctx, c, desired, opts)
	if err != nil {
		return ApplyResult{Created: created}, fmt.Errorf("evaluating managed metadata: %w", err)
	}

	err = retry.OnError(
		retry.DefaultBackoff,
		apierrors.IsConflict,
		func() error {
			return clt.PatchApply(ctx, c, desired, opts.FieldOwner, opts.Force)
		},
	)
	if err != nil {
		return ApplyResult{Created: created}, fmt.Errorf("applying object failed: %w", err)
	}

	err = retry.OnError(
		retry.DefaultBackoff,
		apierrors.IsNotFound,
		func() error {
			return c.Get(ctx, key, actual)
		},
	)
	if err != nil {
		return ApplyResult{Created: created}, fmt.Errorf("failed to get object after apply: %w", err)
	}

	log.FromContext(ctx).V(4).Info("applying managed resource metadata", "patches", len(patches))

	if err := clt.ApplyPatches(
		ctx,
		c,
		actual,
		patches,
		meta.ResourceControllerFieldOwnerPrefix(),
	); err != nil {
		return ApplyResult{Created: created}, fmt.Errorf("applying managed metadata failed: %w", err)
	}

	return ApplyResult{
		LastApply: clt.LastApplyTimeForManager(actual, opts.FieldOwner),
		Created:   created,
	}, nil
}

// Prune relinquishes the fields owned by FieldOwner. Resources known to have
// been created for the manager are deleted; adopted resources retain all
// fields owned by other managers.
func (m Manager) Prune(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	opts PruneOptions,
) (deleted bool, err error) {
	actual, err := m.scopedObjectReference(obj)
	if err != nil {
		return false, err
	}

	if actual.GetNamespace() != "" {
		reader := m.Reader
		if reader == nil {
			reader = c
		}

		ns := &corev1.Namespace{}
		if err := reader.Get(ctx, types.NamespacedName{Name: actual.GetNamespace()}, ns); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}

			return false, err
		}
	}

	if err := c.Get(ctx, client.ObjectKeyFromObject(actual), actual); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	if m.isDeletable(actual, opts) {
		err := c.Delete(ctx, actual)
		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return true, err
	}

	// Applying only the resource identity causes SSA to remove every field
	// previously owned by this manager while preserving other managers' fields.
	prunePatch := objectReference(actual)
	if err := clt.PatchApply(ctx, c, prunePatch, opts.FieldOwner, false); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	return false, nil
}

// Disown removes metadata which is not part of the caller's SSA field set.
// This is used after pruning an adopted or shared resource.
func (m Manager) Disown(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	ownerReference *metav1.OwnerReference,
) error {
	actual, err := m.scopedObjectReference(obj)
	if err != nil {
		return err
	}

	if err := c.Get(ctx, client.ObjectKeyFromObject(actual), actual); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	patches := clt.RemoveOwnerReferencePatch(actual.GetOwnerReferences(), ownerReference)
	if value, ok := actual.GetLabels()[meta.NewManagedByCapsuleLabel]; ok && value == m.Metadata.ManagedByValue {
		patches = append(patches, clt.PatchRemoveLabels(actual.GetLabels(), []string{
			meta.NewManagedByCapsuleLabel,
		})...)
	}

	if err := clt.ApplyPatches(
		ctx,
		c,
		actual,
		patches,
		meta.ResourceControllerFieldOwnerPrefix(),
	); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	return nil
}

// ResolveResourceID returns the canonical identity and scope of a resource as
// it will be managed. In particular, a namespace rendered onto a cluster-scoped
// resource is removed from the returned identity.
func (m Manager) ResolveResourceID(
	obj *unstructured.Unstructured,
	tenant string,
	origin string,
) (gvk.ResourceID, bool, error) {
	scoped, clusterScoped, err := m.scopedObjectWithScope(obj)
	if err != nil {
		return gvk.ResourceID{}, false, err
	}

	return gvk.NewResourceID(scoped, tenant, origin), clusterScoped, nil
}

func (m Manager) managedMetadataPatches(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	opts ApplyOptions,
) (patches []clt.JSONPatch, created bool, err error) {
	existing := obj.DeepCopy()
	err = c.Get(ctx, client.ObjectKeyFromObject(existing), existing)

	switch {
	case apierrors.IsNotFound(err):
		created = true
	case err != nil:
		return nil, false, err
	default:
		created = opts.PreviouslyCreated

		labels := existing.GetLabels()
		if value, ok := labels[meta.CreatedByCapsuleLabel]; ok && value == m.Metadata.CreatedByValue {
			created = true
		}

		if m.Metadata.LegacyCreatedLabel != "" {
			if _, ok := labels[m.Metadata.LegacyCreatedLabel]; ok {
				created = true

				patches = append(patches, clt.PatchRemoveLabels(labels, []string{
					m.Metadata.LegacyCreatedLabel,
				})...)
			}
		}

		// Recover when the initial SSA creation succeeded but the follow-up
		// metadata patch or status update failed. With adoption disabled, this
		// manager could only have acquired fields by creating the resource.
		if !created && !opts.Adopt && hasApplyManager(existing, opts.FieldOwner) {
			created = true
		}
	}

	if !created && !opts.Adopt {
		return nil, false, fmt.Errorf(
			"object %s/%s %s/%s exists and cannot be adopted",
			existing.GetAPIVersion(),
			existing.GetKind(),
			existing.GetNamespace(),
			existing.GetName(),
		)
	}

	if created {
		patches = append(
			patches,
			clt.AddOwnerReferencePatch(existing.GetOwnerReferences(), opts.OwnerReference)...,
		)

		if value, ok := existing.GetLabels()[meta.CreatedByCapsuleLabel]; !ok || value != m.Metadata.CreatedByValue {
			patches = append(patches, clt.AddLabelsPatch(existing.GetLabels(), map[string]string{
				meta.CreatedByCapsuleLabel: m.Metadata.CreatedByValue,
			})...)

			// Keep the local label view in sync so adding the managed-by label
			// does not try to create metadata.labels a second time.
			if existing.GetLabels() == nil {
				existing.SetLabels(map[string]string{
					meta.CreatedByCapsuleLabel: m.Metadata.CreatedByValue,
				})
			}
		}
	}

	if value, ok := existing.GetLabels()[meta.NewManagedByCapsuleLabel]; !ok || value != m.Metadata.ManagedByValue {
		patches = append(patches, clt.AddLabelsPatch(existing.GetLabels(), map[string]string{
			meta.NewManagedByCapsuleLabel: m.Metadata.ManagedByValue,
		})...)
	}

	return patches, created, nil
}

func (m Manager) scopedObjectReference(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	scoped, err := m.scopedObject(obj)
	if err != nil {
		return nil, err
	}

	return objectReference(scoped), nil
}

func (m Manager) scopedObject(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	scoped, _, err := m.scopedObjectWithScope(obj)

	return scoped, err
}

func (m Manager) scopedObjectWithScope(
	obj *unstructured.Unstructured,
) (*unstructured.Unstructured, bool, error) {
	scoped := obj.DeepCopy()
	if m.Mapper == nil {
		return scoped, false, nil
	}

	mapping, err := m.Mapper.RESTMapping(
		obj.GroupVersionKind().GroupKind(),
		obj.GroupVersionKind().Version,
	)
	if err != nil {
		return nil, false, err
	}

	clusterScoped := mapping.Scope.Name() == k8smeta.RESTScopeNameRoot
	if clusterScoped {
		scoped.SetNamespace("")
	}

	return scoped, clusterScoped, nil
}

func (m Manager) isDeletable(actual *unstructured.Unstructured, opts PruneOptions) bool {
	if opts.PreviouslyCreated {
		return true
	}

	if value, ok := actual.GetLabels()[meta.CreatedByCapsuleLabel]; !ok || value != m.Metadata.CreatedByValue {
		return false
	}

	if opts.OwnerReference != nil && meta.HasLooseOwnerReference(actual, *opts.OwnerReference) {
		owners := meta.CapsuleFieldOwners(actual, meta.FieldManagerCapsulePrefix+"/resource/")
		for owner := range owners {
			if owner != opts.FieldOwner && owner != meta.ResourceControllerFieldOwnerPrefix() {
				return false
			}
		}

		return true
	}

	return meta.HasExactlyCapsuleOwners(
		actual,
		meta.FieldManagerCapsulePrefix+"/resource/",
		[]string{opts.FieldOwner, meta.ResourceControllerFieldOwnerPrefix()},
	)
}

func objectReference(obj *unstructured.Unstructured) *unstructured.Unstructured {
	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetName(obj.GetName())
	actual.SetNamespace(obj.GetNamespace())

	return actual
}

func hasApplyManager(obj *unstructured.Unstructured, manager string) bool {
	for _, fields := range obj.GetManagedFields() {
		if fields.Manager == manager && fields.Operation == metav1.ManagedFieldsOperationApply {
			return true
		}
	}

	return false
}
