// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

// TriggerDispatcher runs on every webhook replica. Coalesced annotation patches
// hand notifications to the elected resource controllers without per-request I/O.
type TriggerDispatcher struct {
	client  client.Client
	reader  client.Reader
	log     logr.Logger
	pending *cache.ReplicationNotifications
	metrics *metrics.TriggerRecorder
}

func NewTriggerDispatcher(c client.Client, reader client.Reader, log logr.Logger, recorder *metrics.TriggerRecorder) *TriggerDispatcher {
	return &TriggerDispatcher{client: c, reader: reader, log: log, pending: cache.NewReplicationNotifications(), metrics: recorder}
}

func (d *TriggerDispatcher) NeedLeaderElection() bool { return false }

func (d *TriggerDispatcher) Notify(target replication.Target, interval time.Duration) bool {
	accepted := d.pending.Notify(target, interval, time.Now())

	outcome := "queued"
	if !accepted {
		outcome = "saturated"
	}

	d.metrics.Notifications.WithLabelValues(outcome).Inc()

	return accepted
}

func (d *TriggerDispatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	pruneTicker := time.NewTicker(time.Minute)
	defer pruneTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pruneTicker.C:
			if err := d.prune(ctx); err != nil {
				d.log.Error(err, "cannot prune trigger notifications")
			}
		case now := <-ticker.C:
			// Limit each batch so cancellation and other targets remain responsive.
			for range 32 {
				target, ok := d.pending.Next(now)
				if !ok {
					break
				}

				dispatchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := d.dispatch(dispatchCtx, target)

				cancel()
				d.pending.Complete(target, time.Now(), err != nil)

				if err != nil {
					d.metrics.Notifications.WithLabelValues("retry").Inc()
					d.log.Error(err, "cannot notify replication", "name", target.Name, "namespace", target.Namespace)
				}
			}
		}
	}
}

func (d *TriggerDispatcher) prune(ctx context.Context) error {
	local := &capsulev1beta2.TenantResourceList{}
	global := &capsulev1beta2.GlobalTenantResourceList{}

	filter := client.MatchingFields{replication.TriggerIndexerField: replication.TriggerEnabledKey}
	if err := d.client.List(ctx, local, filter, client.UnsafeDisableDeepCopy); err != nil {
		return err
	}

	if err := d.client.List(ctx, global, filter, client.UnsafeDisableDeepCopy); err != nil {
		return err
	}

	active := make(map[replication.Target]struct{}, len(local.Items)+len(global.Items))

	for i := range local.Items {
		obj := &local.Items[i]
		active[replication.Target{Name: obj.Name, Namespace: obj.Namespace, UID: obj.UID}] = struct{}{}
	}

	for i := range global.Items {
		obj := &global.Items[i]
		active[replication.Target{Name: obj.Name, UID: obj.UID}] = struct{}{}
	}

	d.pending.Prune(active)

	return nil
}

func (d *TriggerDispatcher) dispatch(ctx context.Context, target replication.Target) error {
	var (
		obj  client.Object
		spec *capsulev1beta2.TenantResourceCommonSpec
	)

	if target.Namespace == "" {
		r := &capsulev1beta2.GlobalTenantResource{}
		obj = r
		spec = &r.Spec.TenantResourceCommonSpec
	} else {
		r := &capsulev1beta2.TenantResource{}
		obj = r
		spec = &r.Spec.TenantResourceCommonSpec
	}

	if err := d.reader.Get(ctx, client.ObjectKey{Name: target.Name, Namespace: target.Namespace}, obj); err != nil {
		return client.IgnoreNotFound(err)
	}

	if obj.GetUID() != target.UID || !obj.GetDeletionTimestamp().IsZero() || spec.IsCordoned() || len(spec.Triggers) == 0 {
		return nil
	}

	base, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("unexpected replication object %T", obj)
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[replication.TriggerAnnotation] = string(uuid.NewUUID())
	obj.SetAnnotations(annotations)
	// Optimistic locking never overwrites another replica's notification or a spec edit.
	return d.client.Patch(ctx, obj, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}
