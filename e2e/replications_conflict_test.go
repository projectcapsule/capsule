// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Observe every status transition, including failures later hidden by a successful
// resync. Polling only the final Ready condition would miss the regression.
func observeGlobalReplication(ctx context.Context, name string) func(string) {
	watchClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
	Expect(err).NotTo(HaveOccurred())
	stream, err := watchClient.Watch(ctx, &capsulev1beta2.GlobalTenantResourceList{}, client.MatchingFields{"metadata.name": name})
	Expect(err).NotTo(HaveOccurred())
	var mu sync.Mutex
	versions := map[string]bool{}
	var failures []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range stream.ResultChan() {
			mu.Lock()
			if event.Type == watch.Error {
				failures = append(failures, fmt.Sprintf("watch error: %v", event.Object))
			} else if resource, ok := event.Object.(*capsulev1beta2.GlobalTenantResource); ok {
				versions[resource.ResourceVersion] = true
				for _, condition := range resource.Status.Conditions {
					if condition.Type == meta.ReadyCondition && condition.Status == metav1.ConditionFalse && condition.Reason == meta.FailedReason {
						failures = append(failures, fmt.Sprintf("%s: %s; items=%+v", resource.Name, condition.Message, resource.Status.ProcessedItems))
					}
				}
			}
			mu.Unlock()
		}
	}()
	DeferCleanup(func() { stream.Stop(); <-done })
	return func(version string) {
		Eventually(func() bool {
			mu.Lock()
			defer mu.Unlock()
			return versions[version]
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue(), "watch must catch up before checking intermediate conditions")
		mu.Lock()
		defer mu.Unlock()
		Expect(failures).To(BeEmpty(), "a transient conflict escaped the target retry")
	}
}
