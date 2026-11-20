// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replications

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const releaseNameEnvironment = "CAPSULE_RELEASE_NAME"

// Legacy combined controllers use the core lease. Before starting a separate
// election domain, wait until neither running pods nor their workload templates
// can start a legacy controller, including standby replicas and old ReplicaSets.
type legacyHandoff struct {
	reader    client.Reader
	namespace string
	release   string
	completed chan struct{}
	once      sync.Once
}

func (h *legacyHandoff) wait(ctx context.Context) error {
	previous := ""

	return wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		blocked, err := h.blockedBy(ctx)
		if err != nil {
			return false, fmt.Errorf("check legacy controller handoff: %w", err)
		}

		if blocked != "" {
			if blocked != previous {
				log.FromContext(ctx).Info("waiting for legacy replication controller to stop", "workload", blocked)
				previous = blocked
			}

			return false, nil
		}

		h.once.Do(func() { close(h.completed) })

		return true, nil
	})
}

func (h *legacyHandoff) ready(_ *http.Request) error {
	select {
	case <-h.completed:
		return nil
	default:
		return fmt.Errorf("waiting for legacy replication controllers to stop")
	}
}

func (h *legacyHandoff) blockedBy(ctx context.Context) (string, error) {
	opts := []client.ListOption{client.InNamespace(h.namespace)}
	if h.release != "" {
		opts = append(opts, client.MatchingLabels{"app.kubernetes.io/instance": h.release})
	}

	var deployments appsv1.DeploymentList
	if err := h.reader.List(ctx, &deployments, opts...); err != nil {
		return "", err
	}

	for _, obj := range deployments.Items {
		if (obj.Spec.Replicas == nil || *obj.Spec.Replicas > 0) && legacyManager(obj.Spec.Template.Spec) {
			return "Deployment/" + obj.Name, nil
		}
	}

	var daemons appsv1.DaemonSetList
	if err := h.reader.List(ctx, &daemons, opts...); err != nil {
		return "", err
	}

	for _, obj := range daemons.Items {
		if legacyManager(obj.Spec.Template.Spec) {
			return "DaemonSet/" + obj.Name, nil
		}
	}

	var replicas appsv1.ReplicaSetList
	if err := h.reader.List(ctx, &replicas, opts...); err != nil {
		return "", err
	}

	for _, obj := range replicas.Items {
		if (obj.Spec.Replicas == nil || *obj.Spec.Replicas > 0) && legacyManager(obj.Spec.Template.Spec) {
			return "ReplicaSet/" + obj.Name, nil
		}
	}

	var pods corev1.PodList
	if err := h.reader.List(ctx, &pods, opts...); err != nil {
		return "", err
	}

	for _, obj := range pods.Items {
		if !legacyManager(obj.Spec) || obj.Status.Phase == corev1.PodSucceeded || obj.Status.Phase == corev1.PodFailed {
			continue
		}

		if !obj.DeletionTimestamp.IsZero() {
			stopped := false

			for _, status := range obj.Status.ContainerStatuses {
				if status.Name == "manager" && status.State.Terminated != nil {
					stopped = true
				}
			}

			if stopped {
				continue
			}
		}

		return "Pod/" + obj.Name, nil
	}

	return "", nil
}

func legacyManager(spec corev1.PodSpec) bool {
	for _, container := range spec.Containers {
		if container.Name != "manager" {
			continue
		}

		args := container.Args
		if len(container.Command) > 1 {
			args = container.Command[1:]
		}

		return len(args) == 0 || (args[0] != "core" && args[0] != "replications")
	}

	return false
}

// The manager starts its HTTP probes before the cache and acquires leadership
// after cache synchronization. Gating both cache methods preserves that ordering
// even when no informers have been registered yet, and works without election too.
type handoffCache struct {
	cache.Cache

	handoff *legacyHandoff
}

func (c *handoffCache) Start(ctx context.Context) error {
	if err := c.handoff.wait(ctx); err != nil {
		return err
	}

	return c.Cache.Start(ctx)
}

func (c *handoffCache) WaitForCacheSync(ctx context.Context) bool {
	select {
	case <-c.handoff.completed:
		return c.Cache.WaitForCacheSync(ctx)
	case <-ctx.Done():
		return false
	}
}
