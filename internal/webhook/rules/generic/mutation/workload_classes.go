// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
)

// podPriorityClass applies priority class mutations of one admission request.
// Class objects are read through the injected readers and never retained.
type podPriorityClass struct {
	// reader is a cache: it serves the configured class.
	reader client.Reader
	// fresh is the API server. It decides whether the Pod's own class is the
	// global default, and resolves a configured class the cache does not know yet.
	fresh client.Reader
	// An earlier entry chose the class, so later merges must not treat it as omitted.
	assigned bool
}

func newPodPriorityClass(readers []client.Reader) *podPriorityClass {
	state := &podPriorityClass{}
	if len(readers) > 0 {
		state.reader = readers[0]
	}

	if len(readers) > 1 {
		state.fresh = readers[1]
	}

	return state
}

// mutate applies the class of one mutation entry that selects the Pod.
func (p *podPriorityClass) mutate(ctx context.Context, pod *corev1.Pod, mutation *apirules.NamespaceRuleMutation) error {
	name := mutation.Workloads.Classes.Priority
	if name == "" || !mutation.Workloads.GetWorkloadTargets(apirules.ValidatePod) {
		return nil
	}

	return p.apply(ctx, pod, name, mutation.Action == apirules.MutationActionReplace)
}

// apply sets the class with the priority and preemptionPolicy of its
// PriorityClass. The built-in Priority admission plugin resolves these before
// webhooks run and again after a webhook changes the Pod, rejecting a priority
// that differs from the class. Merge fills an omitted class, which that plugin
// has already replaced with the cluster's global default class when one exists.
func (p *podPriorityClass) apply(ctx context.Context, pod *corev1.Pod, name string, replace bool) error {
	if !replace && p.assigned {
		return nil
	}

	current := pod.Spec.PriorityClassName

	p.assigned = true

	if current == name {
		return nil
	}

	if !replace && current != "" {
		class, err := p.current(ctx, current)
		if err != nil {
			return err
		}

		// An unknown class is explicit: only the global default counts as omitted.
		if class == nil || !class.GlobalDefault {
			return nil
		}
	}

	class, err := p.configured(ctx, name)
	if err != nil {
		return err
	}

	if class == nil {
		return fmt.Errorf("PriorityClass %q does not exist", name)
	}

	pod.Spec.PriorityClassName = class.Name
	pod.Spec.Priority = new(class.Value)
	pod.Spec.PreemptionPolicy = nil

	if class.PreemptionPolicy != nil {
		pod.Spec.PreemptionPolicy = new(*class.PreemptionPolicy)
	}

	return nil
}

// current returns the class the Pod already names. Whether it is the global
// default decides if merge keeps it, and nothing checks that decision later: a
// class promoted or demoted moments ago must not be judged by a cache that is
// behind, so the API server answers when it is available.
func (p *podPriorityClass) current(ctx context.Context, name string) (*schedulingv1.PriorityClass, error) {
	if p.fresh != nil {
		return p.get(ctx, p.fresh, name)
	}

	return p.get(ctx, p.reader, name)
}

// configured returns the class of the rule. The API server rechecks its
// priority, so the cache is enough; only a class it does not know yet, such as
// one created moments ago, is read from the API server.
func (p *podPriorityClass) configured(ctx context.Context, name string) (*schedulingv1.PriorityClass, error) {
	class, err := p.get(ctx, p.reader, name)
	if err != nil || class != nil || p.fresh == nil {
		return class, err
	}

	return p.get(ctx, p.fresh, name)
}

// get returns nil for a class that does not exist.
func (p *podPriorityClass) get(ctx context.Context, reader client.Reader, name string) (*schedulingv1.PriorityClass, error) {
	if reader == nil {
		return nil, errors.New("PriorityClass lookup is unavailable")
	}

	class, err := utils.GetPriorityClassByName(ctx, reader, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("get PriorityClass %q: %w", name, err)
	}

	return class, nil
}

// mutatePodClasses applies the classes one mutation entry assigns. Most entries
// assign none, so that is settled before either class is looked at. An error
// names the class it concerns.
func mutatePodClasses(ctx context.Context, pod *corev1.Pod, mutation *apirules.NamespaceRuleMutation, priorityClass *podPriorityClass) error {
	if mutation.Workloads.Classes.Priority == "" && mutation.Workloads.Classes.Runtime == "" {
		return nil
	}

	if err := priorityClass.mutate(ctx, pod, mutation); err != nil {
		return fmt.Errorf("priority: %w", err)
	}

	mutatePodRuntimeClass(pod, mutation)

	return nil
}

// mutatePodRuntimeClass applies the runtime class of one mutation entry that
// selects the Pod. Kubernetes has no default RuntimeClass, so merge only fills
// an omitted class and replace sets it on every Pod. Only the name is set
// here: reconcilePodRuntimeClass settles the overhead and the scheduling of
// the class the entries end at, and conditionView shows them settled to the
// conditions of later entries.
func mutatePodRuntimeClass(pod *corev1.Pod, mutation *apirules.NamespaceRuleMutation) {
	name := mutation.Workloads.Classes.Runtime
	if name == "" || !mutation.Workloads.GetWorkloadTargets(apirules.ValidatePod) {
		return
	}

	current := ptr.Deref(pod.Spec.RuntimeClassName, "")
	if current == name || (current != "" && mutation.Action != apirules.MutationActionReplace) {
		return
	}

	pod.Spec.RuntimeClassName = new(name)
}

// conditionView returns the Pod the conditions of one entry evaluate against.
// The overhead and the scheduling of a class an earlier entry assigned are
// shown as settled, so conditions see the Pod as the documented mutation order
// leaves it, while the Pod itself keeps only the outcome of the whole pass: a
// class replaced in turn leaves no constraints behind. A class that does not
// exist or that conflicts changes nothing here — the closing reconciliation
// decides whether the Pod is rejected — but a failed lookup propagates:
// evaluated against the unresolved Pod, a condition could silently select a
// different policy on an API error.
func conditionView(ctx context.Context, pod *corev1.Pod, arrival string, before *apirules.WorkloadMutation, readers []client.Reader, mutation *apirules.NamespaceRuleMutation) (*corev1.Pod, error) {
	if len(mutation.Conditions) == 0 || ptr.Deref(pod.Spec.RuntimeClassName, "") == arrival {
		return pod, nil
	}

	view := *pod
	// The merge writes the view's selector in place; every other field it
	// touches is replaced, never edited, so nothing else is shared.
	view.Spec.NodeSelector = maps.Clone(pod.Spec.NodeSelector)

	if _, err := reconcilePodRuntimeClass(ctx, &view, arrival, before, readers); err != nil {
		if _, failed := errors.AsType[*runtimeClassLookupError](err); failed {
			return nil, err
		}

		return pod, nil
	}

	return &view, nil
}

// reconcilePodRuntimeClass applies the overhead and the scheduling of the
// runtime class the mutations assigned, as the built-in RuntimeClass admission
// plugin does for the class a Pod names. That plugin runs before webhooks and
// once more after the first webhook pass changed the Pod, but not after a
// class assigned during the webhook's own reinvocation: left to the plugin, a
// replacement class with scheduling constraints and no overhead would pass
// validation without its node selector and tolerations, and the Pod could be
// scheduled onto incompatible nodes. The merge repeats what the plugin does,
// so an assignment on either call ends with the same Pod and the plugin finds
// nothing left to add. Like the plugin, it rejects a class that does not exist
// and a nodeSelector that conflicts with the Pod's.
//
// The scheduling of a class the Pod kept is restored the same way when the
// entries changed the Pod's node selector or tolerations, against before, the
// placement the Pod arrived with: a replacement can silently drop the selector
// that confines the Pod to compatible nodes, and on reinvocation no later
// plugin pass repairs that. The overhead stays as it is then — no entry
// touched it.
//
// Only the outcome counts, not each assignment: entries that replace the class
// in turn end where the Pod started, and nothing is read or merged. The
// conditions of later entries still see each intermediate class settled,
// through the view of conditionView. The constraints merged for a replaced
// class stay on the Pod only when the Pod arrived with them: they cannot be
// told apart from the Pod's own, and removing them could lift a restriction
// another mutation relies on.
//
// It reports whether the runtime class changed. The class is read only for an
// assignment or a disturbed placement.
func reconcilePodRuntimeClass(ctx context.Context, pod *corev1.Pod, arrival string, before *apirules.WorkloadMutation, readers []client.Reader) (bool, error) {
	name := ptr.Deref(pod.Spec.RuntimeClassName, "")
	if name == "" {
		return false, nil
	}

	assigned := name != arrival
	if !assigned {
		selectorPreserved := true

		for key, value := range before.Placement.NodeSelector {
			if current, ok := pod.Spec.NodeSelector[key]; !ok || current != value {
				selectorPreserved = false

				break
			}
		}

		originalTolerations := before.Placement.Tolerations
		tolerationsPreserved := len(originalTolerations) == 0 ||
			(len(pod.Spec.Tolerations) >= len(originalTolerations) &&
				equality.Semantic.DeepEqual(originalTolerations, pod.Spec.Tolerations[:len(originalTolerations)]))

		if selectorPreserved && tolerationsPreserved {
			return false, nil
		}
	}

	class, err := getRuntimeClass(ctx, readers, name)
	if err != nil {
		return false, err
	}

	if class == nil {
		return false, fmt.Errorf("RuntimeClass %q not found", name)
	}

	if assigned {
		// The plugin rejects an overhead that differs from the class the Pod names.
		pod.Spec.Overhead = nil
		if class.Overhead != nil {
			pod.Spec.Overhead = class.Overhead.PodFixed.DeepCopy()
		}
	}

	if class.Scheduling == nil {
		return assigned, nil
	}

	for key, value := range class.Scheduling.NodeSelector {
		if existing, ok := pod.Spec.NodeSelector[key]; ok && existing != value {
			return false, fmt.Errorf("conflict: runtimeClass.scheduling.nodeSelector[%s] = %s; pod.spec.nodeSelector[%s] = %s", key, value, key, existing)
		}

		if pod.Spec.NodeSelector == nil {
			pod.Spec.NodeSelector = make(map[string]string, len(class.Scheduling.NodeSelector))
		}

		pod.Spec.NodeSelector[key] = value
	}

	if len(class.Scheduling.Tolerations) > 0 {
		pod.Spec.Tolerations = mergeRuntimeClassTolerations(pod.Spec.Tolerations, class.Scheduling.Tolerations)
	}

	return assigned, nil
}

// mergeRuntimeClassTolerations merges the tolerations of the class into the
// Pod's, as the plugin does: a toleration of the class is left out when a kept
// one tolerates a superset of it, and it replaces the kept tolerations it
// covers. A toleration of the class never narrows a broader one the Pod
// carries, such as an unlimited NoExecute toleration against one the class
// limits in time, which would let the Pod be evicted earlier. The Pod's own
// tolerations are never measured against one another: the list stays as its
// author wrote it, and the work stays proportional to what the class adds
// instead of the square of what the Pod carries.
func mergeRuntimeClassTolerations(pod, class []corev1.Toleration) []corev1.Toleration {
	merged := make([]corev1.Toleration, 0, len(pod)+len(class))
	merged = append(merged, pod...)

	for _, toleration := range class {
		covered := slices.ContainsFunc(merged, func(kept corev1.Toleration) bool {
			return toleratesSuperset(kept, toleration)
		})

		merged = slices.DeleteFunc(merged, func(kept corev1.Toleration) bool {
			return toleratesSuperset(toleration, kept) && !toleratesSuperset(kept, toleration)
		})
		if !covered {
			merged = append(merged, *toleration.DeepCopy())
		}
	}

	return merged
}

// toleratesSuperset reports whether ss tolerates a superset of t, with the
// comparison of the plugin.
func toleratesSuperset(ss, t corev1.Toleration) bool {
	if equality.Semantic.DeepEqual(&t, &ss) {
		return true
	}

	if t.Key != ss.Key &&
		// An empty key with Exists matches every key and value.
		(ss.Key != "" || ss.Operator != corev1.TolerationOpExists) {
		return false
	}

	// An empty effect matches every effect.
	if t.Effect != ss.Effect && ss.Effect != "" {
		return false
	}

	if ss.Effect == corev1.TaintEffectNoExecute && ss.TolerationSeconds != nil {
		if t.TolerationSeconds == nil || *t.TolerationSeconds > *ss.TolerationSeconds {
			return false
		}
	}

	switch ss.Operator {
	case corev1.TolerationOpEqual, "": // An empty operator means Equal.
		return t.Operator == corev1.TolerationOpEqual && t.Value == ss.Value
	case corev1.TolerationOpExists:
		return true
	case corev1.TolerationOpLt, corev1.TolerationOpGt:
		// Numeric bounds are not ordered here: only the identical toleration,
		// settled above, collapses. Keeping both preserves the union.
		return false
	default:
		return false
	}
}

// runtimeClassLookupError is a failed read of a RuntimeClass: a server error,
// never a state of the class or a rule decision.
type runtimeClassLookupError struct{ err error }

func (e *runtimeClassLookupError) Error() string { return e.err.Error() }

func (e *runtimeClassLookupError) Unwrap() error { return e.err }

// getRuntimeClass resolves the class through the last of the given readers,
// the authoritative one. The merge is the only application of the class's
// scheduling when the assignment happens on reinvocation, after the plugin's
// final pass, and nothing downstream rejects outdated constraints: unlike the
// priority value and the overhead, which the API server rechecks against the
// live class, a stale node selector would be admitted. A cache hit must
// therefore not shortcut the read; it answers only callers without an API
// reader. This also finds a class created moments ago. It returns nil for a
// class that does not exist, and a runtimeClassLookupError for a read that
// failed.
func getRuntimeClass(ctx context.Context, readers []client.Reader, name string) (*nodev1.RuntimeClass, error) {
	for _, reader := range slices.Backward(readers) {
		if reader == nil {
			continue
		}

		class := &nodev1.RuntimeClass{}
		if err := reader.Get(ctx, client.ObjectKey{Name: name}, class); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}

			return nil, &runtimeClassLookupError{err: fmt.Errorf("get RuntimeClass %q: %w", name, err)}
		}

		return class, nil
	}

	return nil, &runtimeClassLookupError{err: errors.New("RuntimeClass lookup is unavailable")}
}
