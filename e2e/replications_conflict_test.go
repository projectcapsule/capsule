// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
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

// Inject the concurrent write immediately before the real API patch, rather than
// manufacturing a Conflict error: JSON tests return a different status code.
func exerciseReplicationMetadataConflicts(namespace, excludedNamespace string, excludedOwner rbac.UserSpec) {
	ctx := context.Background()
	base := replicationConflictClient()
	conditions, err := cache.NewCELCache()
	Expect(err).NotTo(HaveOccurred())
	p := processor.Processor{GatherClient: base, Mapper: base.RESTMapper(), Conditions: conditions}
	for _, stage := range []string{"metadata", "skipped-policy", "invalid"} {
		By("checking real API responses for " + stage)
		name := "version-conflict-" + rand.String(6)
		excluded := &corev1.ConfigMap{Name: name, Namespace: excludedNamespace, Data: map[string]string{"external": "retained"}}
		actor := impersonationClient(excludedOwner.Name, withDefaultGroups([]string{excludedOwner.Name}))
		EventuallyCreation(func() error { return actor.Create(ctx, excluded) }).Should(Succeed())
		desired := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": name, "namespace": namespace},
			"data": map[string]any{"key": "retained"},
		}}
		owner := meta.ResourceFieldOwner("e2e-" + name)
		policy := &apiruntime.ResourceReplicationPolicy{Protect: new(false)}
		if stage == "skipped-policy" {
			_, err := p.Apply(ctx, base, desired, owner, false, false, nil, nil, policy)
			Expect(err).NotTo(HaveOccurred())
			policy.Condition, policy.Protect = "false", new(true)
			Expect(unstructured.SetNestedField(desired.Object, "must-not-apply", "data", "key")).To(Succeed())
		}
		patches, reads := 0, 0
		racing := interceptor.NewClient(base, interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				reads++
				return c.Get(ctx, key, obj, opts...)
			},
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				manager := (&client.PatchOptions{}).ApplyOptions(opts).FieldManager
				if patch.Type() != types.JSONPatchType || manager != meta.ResourceControllerFieldOwnerPrefix() {
					return c.Patch(ctx, obj, patch, opts...)
				}
				patches++
				if stage == "invalid" {
					err := c.Patch(ctx, obj, client.RawPatch(types.JSONPatchType, []byte(`[{"op":"add","path":"/metadata/labels/invalid key","value":"value"}]`)), opts...)
					Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected an actual API validation error: %v", err)
					return err
				}
				if patches == 1 {
					Eventually(func() error {
						current := &corev1.ConfigMap{}
						if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
							return err
						}
						if current.Annotations == nil {
							current.Annotations = map[string]string{}
						}
						current.Annotations["external.example/concurrent"] = "retained"
						return c.Update(ctx, current)
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					err := c.Patch(ctx, obj, patch, opts...)
					Expect(apierrors.IsConflict(err)).To(BeTrue(), "expected HTTP 409 from a real stale patch: %v", err)
					return err
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		})
		result, err := p.Apply(ctx, racing, desired, owner, false, false, nil, nil, policy)
		if stage == "invalid" {
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(patches).To(Equal(1), "genuine validation failures must not be retried")
			Expect(reads).To(Equal(1))
		} else {
			Expect(err).NotTo(HaveOccurred())
			Expect(patches).To(Equal(2))
			Expect(reads).To(Equal(2), "each retry must read fresh state without extra lookups")
			Expect(result.Skipped).To(Equal(stage == "skipped-policy"))
		}
		actual := &corev1.ConfigMap{}
		Expect(base.Get(ctx, client.ObjectKeyFromObject(desired), actual)).To(Succeed())
		Expect(result.UID).To(Equal(actual.UID))
		Expect(result.LastApply).NotTo(BeNil())
		Expect(actual.Data).To(Equal(map[string]string{"key": "retained"}))
		Expect(actual.Labels).NotTo(HaveKey("invalid key"))
		if stage != "invalid" {
			Expect(actual.Annotations).To(HaveKeyWithValue("external.example/concurrent", "retained"))
		}
		expectConfigMapData(excludedNamespace, name, map[string]string{"external": "retained"})
	}
}

func exerciseReplicationExhaustedConflicts(tenant, namespace, excludedNamespace string, owner, excludedOwner rbac.UserSpec) {
	ctx := context.Background()
	base := replicationConflictClient()
	p := processor.Processor{GatherClient: base, Mapper: base.RESTMapper()}
	for _, deletion := range []apiruntime.ResourceDeletionPolicy{apiruntime.ResourceDeletionPolicyRemove, apiruntime.ResourceDeletionPolicyOrphan} {
		name := "exhausted-conflict-" + rand.String(6)
		excludedActor := impersonationClient(excludedOwner.Name, withDefaultGroups([]string{excludedOwner.Name}))
		excluded := &corev1.ConfigMap{Name: name, Namespace: excludedNamespace, Data: map[string]string{"external": "retained"}}
		EventuallyCreation(func() error { return excludedActor.Create(ctx, excluded) }).Should(Succeed())
		desired := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": name, "namespace": namespace},
			"data": map[string]any{"key": "retained"},
		}}
		attempts := 0
		racing := interceptor.NewClient(base, interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if patch.Type() != types.ApplyPatchType {
				return c.Patch(ctx, obj, patch, opts...)
			}
			attempts++
			Eventually(func() error {
				current := &corev1.ConfigMap{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
					return err
				}
				if current.Annotations == nil {
					current.Annotations = map[string]string{}
				}
				current.Annotations["external.example/attempt"] = fmt.Sprint(attempts)
				return c.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			err := c.Patch(ctx, obj, patch, opts...)
			Expect(apierrors.IsConflict(err)).To(BeTrue(), "expected a real SSA version conflict: %v", err)
			return err
		}})
		id := gvk.NewResourceID(desired, tenant, "0/raw-0")
		policy := &apiruntime.ResourceReplicationPolicy{Protect: new(false), Deletion: deletion}
		acc := processor.Accumulator{}
		processor.AccumulatorAdd(acc, id, processor.AccumulatorObject{Object: desired, Policy: policy})
		processed := meta.ProcessedItems{}
		opts := processor.ProcessorOptions{FieldOwnerPrefix: meta.ResourceFieldOwner(name), Prune: true}
		Expect(p.Reconcile(ctx, logr.Discard(), racing, &processed, acc, opts)).To(MatchError(ContainSubstring("applying of 1 resources failed")))
		Expect(attempts).To(Equal(retry.DefaultBackoff.Steps))
		Expect(processed).To(HaveLen(1))
		Expect(processed[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(processed[0].Message).To(ContainSubstring("the object has been modified"))
		Expect(processed[0].Created).To(BeTrue())
		Expect(processed[0].Policy.Deletion).To(Equal(deletion))
		actual := &corev1.ConfigMap{}
		Expect(base.Get(ctx, client.ObjectKeyFromObject(desired), actual)).To(Succeed())
		Expect(processed[0].LastApply.Equal(&actual.CreationTimestamp)).To(BeTrue(), "creation must remain tracked after every SSA attempt fails")
		By("removing the failed item before any successful resync")
		Expect(p.Reconcile(ctx, logr.Discard(), base, &processed, processor.Accumulator{}, opts)).To(Succeed())
		Expect(processed).To(BeEmpty())
		if deletion == apiruntime.ResourceDeletionPolicyRemove {
			Expect(apierrors.IsNotFound(base.Get(ctx, client.ObjectKeyFromObject(desired), actual))).To(BeTrue())
		} else {
			Expect(base.Get(ctx, client.ObjectKeyFromObject(desired), actual)).To(Succeed())
			Expect(actual.Data).To(Equal(map[string]string{"key": "retained"}))
			actor := impersonationClient(owner.Name, withDefaultGroups([]string{owner.Name}))
			Eventually(func() error {
				if err := actor.Get(ctx, client.ObjectKeyFromObject(desired), actual); err != nil {
					return err
				}
				return actor.Patch(ctx, actual, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"owner":"allowed"}}}`)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(base.Get(ctx, client.ObjectKeyFromObject(desired), actual)).To(Succeed())
			Expect(actual.Annotations).To(HaveKeyWithValue("owner", "allowed"))
		}
		expectConfigMapData(excludedNamespace, name, map[string]string{"external": "retained"})
	}
}

// Replication writes use the controller identity, which is authorized to create
// lifecycle markers. Keep ordinary tenant-owner clients for tenant operations.
func replicationConflictClient() client.WithWatch {
	controllerConfig := rest.CopyConfig(cfg)
	controllerConfig.Impersonate = rest.ImpersonationConfig{UserName: ControllerServiceAccountFull, Groups: serviceAccountGroups(ControllerNamespace)}
	c, err := client.NewWithWatch(controllerConfig, client.Options{Scheme: k8sClient.Scheme()})
	Expect(err).NotTo(HaveOccurred())
	return c
}
