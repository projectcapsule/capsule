// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/cel/environment"
	"k8s.io/client-go/util/csaupgrade"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
)

const ConditionNotMet = "ConditionNotMet: apply skipped"

func (m Manager) checkCondition(ctx context.Context, c client.Client, desired *unstructured.Unstructured, opts ApplyOptions) (*unstructured.Unstructured, ApplyResult, error) {
	result := ApplyResult{Created: opts.PreviouslyCreated}
	if m.Conditions == nil {
		return nil, result, fmt.Errorf("resource condition compiler is not configured")
	}

	compiled, err := m.Conditions.GetOrCompileResourceCondition(opts.Condition, environment.StoredExpressions)
	if err != nil {
		return nil, result, fmt.Errorf("invalid resource condition: %w", err)
	}

	existing := objectReference(desired)

	var object any

	if err := c.Get(ctx, client.ObjectKeyFromObject(existing), existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, result, fmt.Errorf("reading condition target: %w", err)
		}

		existing = nil
	} else {
		object = existing.Object
	}

	allowed, err := compiled.EvaluateBooleanWithVariables(ctx, map[string]any{"object": object, "now": time.Now().UTC()})
	if err != nil {
		// CEL errors may include values from a Secret. Keep those values out of
		// processed-item status, events, and logs.
		return nil, result, fmt.Errorf("ConditionEvaluationFailed: expression could not be evaluated")
	}

	if !allowed {
		return existing, m.skippedResult(existing, opts), nil
	}

	return existing, result, nil
}

func (m Manager) skippedResult(existing *unstructured.Unstructured, opts ApplyOptions) ApplyResult {
	result := ApplyResult{Skipped: true, Created: opts.PreviouslyCreated}
	if existing == nil {
		return result
	}

	result.LastApply = clt.LastApplyTimeForManager(existing, opts.FieldOwner)
	managedByUs := hasFieldManager(existing, opts.FieldOwner)

	createdByUs := managedByUs && m.Metadata.CreatedByValue != "" && existing.GetLabels()[meta.CreatedByCapsuleLabel] == m.Metadata.CreatedByValue
	// Creation policy can change after adoption. Field ownership alone does not
	// prove that we created an object and must never authorize deleting it.
	result.Created = result.Created || createdByUs

	if result.LastApply == nil && createdByUs {
		// Recover an initialization that succeeded before SSA or status failed.
		for _, field := range existing.GetManagedFields() {
			if field.Manager == opts.FieldOwner && field.Time != nil {
				result.LastApply = field.Time.DeepCopy()
			}
		}
	}

	return result
}

// A false condition gates rendered content, not policy on an already managed
// target. Reuse the condition read and never acquire a previously unowned target.
func (m Manager) reconcileSkippedPolicy(ctx context.Context, c client.Client, existing *unstructured.Unstructured, opts ApplyOptions) (bool, error) {
	if existing == nil || !hasFieldManager(existing, opts.FieldOwner) {
		return false, nil
	}

	patches, err := m.protectionPatches(ctx, existing, opts.Protect, opts.FieldOwner, nil)
	if err != nil {
		return false, err
	}

	if len(patches) == 0 {
		return true, nil
	}

	// Protect against replacement or metadata changes since the condition read.
	// Retry through reconciliation so the next attempt checks current ownership.
	patches = append([]clt.JSONPatch{{Operation: "test", Path: "/metadata/resourceVersion", Value: existing.GetResourceVersion()}}, patches...)

	rawPatch, err := clt.JSONPatchesToRawPatch(patches)
	if err != nil {
		return false, err
	}

	options := []client.PatchOption{client.FieldOwner(meta.ResourceControllerFieldOwnerPrefix())}
	if opts.DryRun {
		options = append(options, client.DryRunAll)
	}

	if err := c.Patch(ctx, existing, client.RawPatch(types.JSONPatchType, rawPatch), options...); err != nil {
		return false, fmt.Errorf("reconciling skipped target policy: %w", err)
	}

	return true, nil
}

// Reuse the condition snapshot or the ordinary adoption read to prepare
// compatibility metadata without an additional API lookup.
func (m Manager) prepareProtection(ctx context.Context, c client.Client, desired *unstructured.Unstructured, opts ApplyOptions, snapshot []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	if len(snapshot) == 0 {
		existing := objectReference(desired)
		if err := c.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("reading managed target: %w", err)
			}

			existing = nil
		}

		snapshot = append(snapshot, existing)
	}
	// Preserve a different controller's legacy identity even when force is set.
	// Our own compatibility label stays in the SSA field set alongside its
	// execution identity, so pruning cannot leave it without authorization.
	if opts.Protect && m.Metadata.ProtectedByValue != "" {
		existing := snapshot[0]
		if existing == nil || existing.GetLabels()[meta.ProtectedByCapsuleLabel] == "" || existing.GetLabels()[meta.ProtectedByCapsuleLabel] == m.Metadata.ProtectedByValue {
			labels := desired.GetLabels()
			labels[meta.ProtectedByCapsuleLabel] = m.Metadata.ProtectedByValue
			desired.SetLabels(labels)
		}
	}

	return snapshot, nil
}

func (m Manager) protectionPatches(ctx context.Context, existing *unstructured.Unstructured, protect bool, fieldOwner string, owners map[string]struct{}) (patches []clt.JSONPatch, err error) {
	labels := existing.GetLabels()
	annotations := existing.GetAnnotations()
	annotation := m.Metadata.ProtectedByServiceAccountAnnotation

	if protect {
		if m.Metadata.ProtectedByValue != "" {
			add := map[string]string{meta.ProtectionLabelPrefix + m.Metadata.ProtectedByValue: meta.ValueTrue}
			// Keep legacy admission identities, including those installed by a
			// different controller. Each controller now has its own marker.
			if labels[meta.ProtectedByCapsuleLabel] == "" {
				add[meta.ProtectedByCapsuleLabel] = m.Metadata.ProtectedByValue
			}

			patches = append(patches, clt.AddLabelsPatch(labels, add)...)
		}

		if annotation != "" && m.Metadata.ProtectedByServiceAccount != "" {
			patches = append(patches, clt.AddAnnotationsPatch(annotations, map[string]string{annotation: m.Metadata.ProtectedByServiceAccount})...)
		}

		return patches, nil
	}

	if m.Metadata.ProtectedByValue != "" {
		patches = append(patches, m.removeProtectionLabels(labels)...)
	}

	if annotation != "" {
		patches = append(patches, clt.PatchRemoveAnnotations(annotations, []string{annotation})...)
	}

	if len(patches) > 0 {
		if owners == nil {
			owners, err = m.resourceFieldOwners(ctx, existing, fieldOwner)
			if err != nil {
				return nil, err
			}
		}

		if m.hasOtherProtectionOwners(owners, fieldOwner) {
			return nil, nil
		}
	}

	return patches, nil
}

func (m Manager) removeProtectionLabels(labels map[string]string) []clt.JSONPatch {
	patches := clt.PatchRemoveLabels(labels, []string{meta.ProtectionLabelPrefix + m.Metadata.ProtectedByValue})
	if labels[meta.ProtectedByCapsuleLabel] != m.Metadata.ProtectedByValue {
		return patches
	}

	// Restore the compatibility marker to a surviving controller.
	for _, controller := range []string{meta.ValueControllerResourcePermit, meta.ValueControllerReplications} {
		if controller != m.Metadata.ProtectedByValue && labels[meta.ProtectionLabelPrefix+controller] == meta.ValueTrue {
			return append(patches, clt.AddLabelsPatch(labels, map[string]string{meta.ProtectedByCapsuleLabel: controller})...)
		}
	}

	return append(patches, clt.PatchRemoveLabels(labels, []string{meta.ProtectedByCapsuleLabel})...)
}

// Owners of the same controller share a protection marker. Owners from a
// different controller have an independent marker and authorization metadata.
// Unknown Capsule owner formats retain the conservative compatibility behavior.
func (m Manager) hasOtherProtectionOwners(owners map[string]struct{}, fieldOwner string) bool {
	for owner := range owners {
		if owner == fieldOwner || owner == meta.ResourceControllerFieldOwnerPrefix() {
			continue
		}

		switch m.Metadata.ProtectedByValue {
		case meta.ValueControllerReplications:
			if strings.HasPrefix(owner, meta.ResourceFieldOwner("resourcepermit/")) {
				continue
			}
		case meta.ValueControllerResourcePermit:
			if !strings.HasPrefix(owner, meta.ResourceFieldOwner("")) {
				continue
			}
		}

		return true
	}

	return false
}

// Shared tracking metadata cannot identify which resource manager still needs
// it. Preserve it conservatively while another Capsule resource owner remains;
// the last departing owner can remove it. Protection has a separate guard per
// controller. External field managers and the lifecycle manager do not count.
func hasOtherResourceOwners(owners map[string]struct{}, fieldOwner string) bool {
	for owner := range owners {
		if owner != fieldOwner && owner != meta.ResourceControllerFieldOwnerPrefix() {
			return true
		}
	}

	return false
}

func hasFieldManager(obj *unstructured.Unstructured, manager string) bool {
	return slices.ContainsFunc(obj.GetManagedFields(), func(field metav1.ManagedFieldsEntry) bool {
		return field.Manager == manager
	})
}

func (m Manager) createConditionalTarget(ctx context.Context, c client.Client, desired *unstructured.Unstructured, opts ApplyOptions) (*metav1.Time, error) {
	initial := desired.DeepCopy()

	labels := initial.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	// Recover an interrupted initialization before the first SSA/status write.
	labels[meta.CreatedByCapsuleLabel] = m.Metadata.CreatedByValue
	initial.SetLabels(labels)

	options := []client.CreateOption{client.FieldOwner(opts.FieldOwner)}
	if opts.DryRun {
		options = append(options, client.DryRunAll)
	}

	if err := c.Create(ctx, initial, options...); err != nil {
		return nil, fmt.Errorf("creating conditional target: %w", err)
	}

	created := initial.GetCreationTimestamp()

	if !opts.DryRun {
		if err := m.upgradeConditionalOwnership(ctx, c, initial, opts); err != nil {
			return &created, err
		}
	}

	desired.SetResourceVersion(initial.GetResourceVersion())
	desired.SetUID(initial.GetUID())

	return &created, nil
}

// Migrate only this controller's create-only initialization to Apply ownership.
// The Kubernetes helper includes a resource-version precondition and preserves
// other managers. Without migration, future rotations conflict with the create.
func (m Manager) upgradeConditionalOwnership(ctx context.Context, c client.Client, existing *unstructured.Unstructured, opts ApplyOptions) error {
	if m.Metadata.CreatedByValue == "" || existing.GetLabels()[meta.CreatedByCapsuleLabel] != m.Metadata.CreatedByValue {
		return nil
	}

	patch, err := csaupgrade.UpgradeManagedFieldsPatch(existing, sets.New(opts.FieldOwner), opts.FieldOwner)
	if err != nil {
		return fmt.Errorf("preparing conditional SSA ownership: %w", err)
	}

	if patch == nil {
		return nil
	}

	options := []client.PatchOption{client.FieldOwner(opts.FieldOwner)}
	if opts.DryRun {
		options = append(options, client.DryRunAll)
	}

	if err := c.Patch(ctx, existing, client.RawPatch(types.JSONPatchType, patch), options...); err != nil {
		return fmt.Errorf("initializing conditional SSA ownership: %w", err)
	}

	return nil
}
