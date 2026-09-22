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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

// Metadata configures the labels used to track resources managed by a Manager.
type Metadata struct {
	CreatedByValue                      string
	ManagedByValue                      string
	ProtectedByValue                    string
	ProtectedByServiceAccount           string
	ProtectedByServiceAccountAnnotation string
	AppManagedByValue                   string
	LegacyCreatedLabel                  string
}

// Manager applies and prunes resources with server-side apply.
type Manager struct {
	Reader     client.Reader
	Mapper     k8smeta.RESTMapper
	Metadata   Metadata
	Conditions celruntime.ResourceConditionCompiler
}

// ApplyOptions configures one server-side apply operation.
type ApplyOptions struct {
	Condition string
	// ExpectedResourceVersion binds a stateful render to its context snapshot.
	// A non-nil empty version requires the destination to still be absent.
	ExpectedResourceVersion *string
	FieldOwner              string
	Force                   bool
	Adopt                   bool
	Protect                 bool
	DryRun                  bool
	OwnerReference          *metav1.OwnerReference
	PreviouslyCreated       bool
}

// ApplyResult describes the resource after a successful apply.
type ApplyResult struct {
	LastApply *metav1.Time
	Created   bool
	Skipped   bool
	// PolicyReconciled means a skipped target is owned by this field manager
	// and its lifecycle metadata reflects the requested policy.
	PolicyReconciled bool
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
		meta.CreatedByCapsuleLabel:         {},
		meta.NewManagedByCapsuleLabel:      {},
		meta.ProtectedByCapsuleLabel:       {},
		meta.ReplicationProtectionLabel:    {},
		meta.ResourcePermitProtectionLabel: {},
	})

	protectionAnnotation := m.Metadata.ProtectedByServiceAccountAnnotation
	if protectionAnnotation != "" {
		annotations := desired.GetAnnotations()
		delete(annotations, protectionAnnotation)

		if opts.Protect && m.Metadata.ProtectedByServiceAccount != "" {
			if annotations == nil {
				annotations = map[string]string{}
			}

			annotations[protectionAnnotation] = m.Metadata.ProtectedByServiceAccount
		}

		desired.SetAnnotations(annotations)
	}

	if opts.Protect && m.Metadata.ProtectedByValue != "" {
		labels := desired.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		labels[meta.ProtectionLabelPrefix+m.Metadata.ProtectedByValue] = meta.ValueTrue
		desired.SetLabels(labels)
	}

	actual := objectReference(desired)
	key := client.ObjectKeyFromObject(actual)

	var snapshot []*unstructured.Unstructured

	if opts.Condition != "" {
		existing, result, err := m.checkCondition(ctx, c, desired, opts)
		if err != nil {
			return result, err
		}

		if result.Skipped {
			result.PolicyReconciled, err = m.reconcileSkippedPolicy(ctx, c, existing, opts)

			return result, err
		}

		snapshot = append(snapshot, existing)

		if existing != nil {
			err = m.upgradeConditionalOwnership(ctx, c, existing, opts)
			desired.SetResourceVersion(existing.GetResourceVersion())
			desired.SetUID(existing.GetUID())
		}

		if err != nil {
			return result, err
		}
	}

	snapshot, err = m.prepareProtection(ctx, c, desired, opts, snapshot)
	if err != nil {
		return ApplyResult{}, err
	}

	_, created, err := m.managedMetadataPatches(ctx, c, desired, opts, snapshot...)
	if err != nil {
		return ApplyResult{Created: created}, fmt.Errorf("evaluating managed metadata: %w", err)
	}

	var initialized *metav1.Time

	if opts.Condition != "" && snapshot[0] == nil {
		// Create is the absent-object precondition: an SSA upsert could overwrite
		// a concurrent creation. Establish SSA ownership immediately afterwards.
		initialized, err = m.createConditionalTarget(ctx, c, desired, opts)
		if err != nil {
			return ApplyResult{Created: initialized != nil, LastApply: initialized}, err
		}

		if opts.DryRun {
			return ApplyResult{Created: true}, nil
		}
	}

	apply := func() error { return clt.PatchApply(ctx, c, desired, opts.FieldOwner, opts.Force, opts.DryRun) }
	if opts.Condition != "" {
		// A conflict must restart evaluation and rendering from fresh state.
		err = apply()
	} else {
		err = retry.OnError(retry.DefaultBackoff, apierrors.IsConflict, apply)
	}

	if err != nil {
		return ApplyResult{Created: created, LastApply: initialized}, fmt.Errorf("applying object failed: %w", err)
	}

	// A server-side dry-run exercises discovery, authorization, admission,
	// schema validation, adoption, and SSA conflicts without persisting the
	// desired object. There is consequently no actual object or managed-fields
	// timestamp to read or decorate afterwards.
	if opts.DryRun {
		return ApplyResult{Created: created}, nil
	}

	err = retry.OnError(
		retry.DefaultBackoff,
		apierrors.IsNotFound,
		func() error {
			return c.Get(ctx, key, actual)
		},
	)
	if err != nil {
		return ApplyResult{Created: created, LastApply: initialized}, fmt.Errorf("failed to get object after apply: %w", err)
	}

	lastApply := successfulApplyTime(actual, opts.FieldOwner)

	// Build patches against the resulting object: SSA can add protection
	// markers or relinquish legacy labels. Reuse the read above.
	metadataOptions := opts
	metadataOptions.PreviouslyCreated = created

	patches, created, err := m.managedMetadataPatches(ctx, c, desired, metadataOptions, actual)
	if err != nil {
		return ApplyResult{Created: created, LastApply: lastApply}, err
	}

	// A previous skipped apply may have assigned protection to the lifecycle
	// manager. Omitting it from this manager's SSA payload cannot remove it.
	patches = append(patches, m.protectionPatches(actual, opts.Protect, opts.FieldOwner, nil)...)
	if len(patches) > 0 {
		patches = append([]clt.JSONPatch{{Operation: "test", Path: "/metadata/resourceVersion", Value: actual.GetResourceVersion()}}, patches...)
	}

	log.FromContext(ctx).V(4).Info("applying managed resource metadata", "patches", len(patches))

	if err := clt.ApplyPatches(
		ctx,
		c,
		actual,
		patches,
		meta.ResourceControllerFieldOwnerPrefix(),
	); err != nil {
		return ApplyResult{Created: created, LastApply: lastApply}, fmt.Errorf("applying managed metadata failed: %w", err)
	}

	return ApplyResult{
		LastApply: lastApply,
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
	if err := clt.PatchApply(ctx, c, prunePatch, opts.FieldOwner, false, false); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	return false, nil
}

// Disown removes metadata which is not part of the caller's SSA field set.
// This is used after pruning an adopted or shared resource. fieldOwner identifies
// the departing manager, including legacy cleanup that retains its SSA fields.
func (m Manager) Disown(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
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

	owners := meta.CapsuleResourceFieldOwners(actual)

	patches := clt.RemoveOwnerReferencePatch(actual.GetOwnerReferences(), ownerReference)
	if value, ok := actual.GetLabels()[meta.NewManagedByCapsuleLabel]; ok && value == m.Metadata.ManagedByValue && !hasOtherResourceOwners(owners, fieldOwner) {
		patches = append(patches, clt.PatchRemoveLabels(actual.GetLabels(), []string{
			meta.NewManagedByCapsuleLabel,
		})...)
	}
	// Protection is composed per controller, independently of shared tracking.
	// A skipped apply can leave it owned by the lifecycle manager after pruning.
	patches = append(patches, m.protectionPatches(actual, false, fieldOwner, owners)...)

	if len(patches) > 0 {
		// Another resource manager may have acquired the target since the read.
		patches = append([]clt.JSONPatch{{Operation: "test", Path: "/metadata/resourceVersion", Value: actual.GetResourceVersion()}}, patches...)
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

// Orphan stops lifecycle management without removing the resource or
// relinquishing the fields applied by the previous manager. Capsule tracking
// metadata is removed only when no other Capsule resource field owner remains.
// Protection is removed only when no other owner of this controller remains.
func (m Manager) Orphan(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
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

	owners := meta.CapsuleResourceFieldOwners(actual)

	patches := clt.RemoveOwnerReferencePatch(actual.GetOwnerReferences(), ownerReference)
	if !hasOtherResourceOwners(owners, fieldOwner) {
		patches = append(patches, m.orphanMetadataPatches(actual)...)
	}

	patches = append(patches, m.protectionPatches(actual, false, fieldOwner, owners)...)

	if len(patches) > 0 {
		// Ownership may have changed since the read. Reconcile again instead
		// of removing another manager's lifecycle metadata from stale state.
		patches = append([]clt.JSONPatch{{Operation: "test", Path: "/metadata/resourceVersion", Value: actual.GetResourceVersion()}}, patches...)
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

func (m Manager) orphanMetadataPatches(actual *unstructured.Unstructured) (patches []clt.JSONPatch) {
	labels := actual.GetLabels()
	removeLabels := make([]string, 0, 5)

	if m.Metadata.CreatedByValue != "" {
		if value, ok := labels[meta.CreatedByCapsuleLabel]; ok && value == m.Metadata.CreatedByValue {
			removeLabels = append(removeLabels, meta.CreatedByCapsuleLabel)
		}
	}

	if m.Metadata.ManagedByValue != "" {
		if value, ok := labels[meta.NewManagedByCapsuleLabel]; ok && value == m.Metadata.ManagedByValue {
			removeLabels = append(removeLabels, meta.NewManagedByCapsuleLabel)
		}
	}

	if m.Metadata.AppManagedByValue != "" {
		if value, ok := labels[meta.AppManagedByLabel]; ok && value == m.Metadata.AppManagedByValue {
			removeLabels = append(removeLabels, meta.AppManagedByLabel)
		}
	}

	if m.Metadata.LegacyCreatedLabel != "" {
		if _, ok := labels[m.Metadata.LegacyCreatedLabel]; ok {
			removeLabels = append(removeLabels, m.Metadata.LegacyCreatedLabel)
		}
	}

	patches = append(patches, clt.PatchRemoveLabels(labels, removeLabels)...)

	return patches
}

func (m Manager) managedMetadataPatches(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	opts ApplyOptions,
	snapshot ...*unstructured.Unstructured,
) (patches []clt.JSONPatch, created bool, err error) {
	existing := obj.DeepCopy()

	switch {
	case len(snapshot) == 0:
		err = c.Get(ctx, client.ObjectKeyFromObject(existing), existing)
	case snapshot[0] == nil:
		err = apierrors.NewNotFound(schema.GroupResource{Resource: obj.GetKind()}, obj.GetName())
	default:
		existing = snapshot[0].DeepCopy()
	}

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

func successfulApplyTime(obj *unstructured.Unstructured, manager string) *metav1.Time {
	lastApply := clt.LastApplyTimeForManager(obj, manager)
	if lastApply == nil && hasApplyManager(obj, manager) {
		// SSA can acquire shared fields without changing their values and omit
		// the manager's timestamp. Record the successful apply so cleanup does
		// not mistake this owned target for one that was never applied.
		return new(metav1.Now())
	}

	return lastApply
}

func hasApplyManager(obj *unstructured.Unstructured, manager string) bool {
	for _, fields := range obj.GetManagedFields() {
		if fields.Manager == manager && fields.Operation == metav1.ManagedFieldsOperationApply {
			return true
		}
	}

	return false
}
