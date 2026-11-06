// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
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
// an omitted class and replace sets it on every Pod.
//
// No RuntimeClass is read. The built-in RuntimeClass admission plugin runs
// before webhooks and once more after a webhook changed the Pod: it rejects a
// class that does not exist, then derives the overhead and scheduling of the
// assigned one. The scheduling constraints it merged for a replaced class stay
// on the Pod: they cannot be told apart from the Pod's own, and removing them
// could lift a restriction another mutation relies on.
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

// resetPodOverhead drops spec.overhead when the mutations assigned another
// runtime class than the Pod arrived with. The RuntimeClass admission plugin
// derived it from that class and rejects an overhead that differs from the
// class the Pod names; it derives the overhead of the assigned class again.
//
// Only the outcome counts, not each assignment. The webhook is called again
// once the plugin has set the overhead of the assigned class, and entries that
// replace the class in turn then end where the Pod started: its overhead is the
// right one, and nothing would derive it another time.
//
// That also bounds what conditions can do. A class they select only on that
// second call, from what the plugin or another webhook added to the Pod in
// between, is assigned without the overhead and the scheduling of its
// RuntimeClass. Dropping the overhead keeps the Pod from being admitted with
// that of another class: the plugin still validates it, and rejects the Pod if
// the assigned class defines one.
//
// It reports whether the runtime class changed.
func resetPodOverhead(pod *corev1.Pod, runtimeClass string) bool {
	if ptr.Deref(pod.Spec.RuntimeClassName, "") == runtimeClass {
		return false
	}

	pod.Spec.Overhead = nil

	return true
}
