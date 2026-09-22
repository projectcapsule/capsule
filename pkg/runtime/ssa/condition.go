// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"fmt"
	"slices"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

	if opts.ExpectedResourceVersion != nil {
		version := ""
		if existing != nil {
			version = existing.GetResourceVersion()
		}

		if version != *opts.ExpectedResourceVersion {
			return nil, result, apierrors.NewConflict(schema.GroupResource{Group: desired.GroupVersionKind().Group, Resource: desired.GetKind()}, desired.GetName(), fmt.Errorf("template context changed; render again from the current target"))
		}
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

	patches := m.protectionPatches(existing, opts.Protect)
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

func (m Manager) protectionPatches(existing *unstructured.Unstructured, protect bool) (patches []clt.JSONPatch) {
	labels := existing.GetLabels()
	annotations := existing.GetAnnotations()
	annotation := m.Metadata.ProtectedByServiceAccountAnnotation

	if protect {
		if m.Metadata.ProtectedByValue != "" {
			patches = append(patches, clt.AddLabelsPatch(labels, map[string]string{meta.ProtectedByCapsuleLabel: m.Metadata.ProtectedByValue})...)
		}

		if annotation != "" && m.Metadata.ProtectedByServiceAccount != "" {
			patches = append(patches, clt.AddAnnotationsPatch(annotations, map[string]string{annotation: m.Metadata.ProtectedByServiceAccount})...)
		}

		return patches
	}

	if m.Metadata.ProtectedByValue != "" && labels[meta.ProtectedByCapsuleLabel] == m.Metadata.ProtectedByValue {
		patches = append(patches, clt.PatchRemoveLabels(labels, []string{meta.ProtectedByCapsuleLabel})...)
	}

	if annotation != "" {
		patches = append(patches, clt.PatchRemoveAnnotations(annotations, []string{annotation})...)
	}

	return patches
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
