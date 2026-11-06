// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
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
// an omitted class and replace sets it on every Pod. Only the name is set
// here: reconcilePodRuntimeClass settles the overhead and the scheduling of
// the class the entries end at.
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
// Only the outcome counts, not each assignment: entries that replace the class
// in turn end where the Pod started, and nothing is read or merged. The
// constraints merged for a replaced class stay on the Pod: they cannot be told
// apart from the Pod's own, and removing them could lift a restriction another
// mutation relies on.
//
// It reports whether the runtime class changed. Only then is the class read.
func reconcilePodRuntimeClass(ctx context.Context, pod *corev1.Pod, arrival string, readers []client.Reader) (bool, error) {
	name := ptr.Deref(pod.Spec.RuntimeClassName, "")
	if name == arrival {
		return false, nil
	}

	class, err := getRuntimeClass(ctx, readers, name)
	if err != nil {
		return false, err
	}

	if class == nil {
		return false, fmt.Errorf("RuntimeClass %q not found", name)
	}

	// The plugin rejects an overhead that differs from the class the Pod names.
	pod.Spec.Overhead = nil
	if class.Overhead != nil {
		pod.Spec.Overhead = class.Overhead.PodFixed.DeepCopy()
	}

	if class.Scheduling == nil {
		return true, nil
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

	for _, toleration := range class.Scheduling.Tolerations {
		upsertPlacement(&pod.Spec.Tolerations, *toleration.DeepCopy(), sameToleration)
	}

	return true, nil
}

// getRuntimeClass resolves the class through the cache, falling back to the
// API server for one the cache does not know yet, such as a class created
// moments ago. It returns nil for a class that does not exist.
func getRuntimeClass(ctx context.Context, readers []client.Reader, name string) (*nodev1.RuntimeClass, error) {
	available := false

	for _, reader := range readers {
		if reader == nil {
			continue
		}

		available = true
		class := &nodev1.RuntimeClass{}

		err := reader.Get(ctx, client.ObjectKey{Name: name}, class)
		if err == nil {
			return class, nil
		}

		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get RuntimeClass %q: %w", name, err)
		}
	}

	if !available {
		return nil, errors.New("RuntimeClass lookup is unavailable")
	}

	return nil, nil
}
