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
