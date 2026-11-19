// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package indexers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/customquota"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/ingress"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/namespace"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/resourcepool"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantowner"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/utils"
)

type CustomIndexer interface {
	Object() client.Object
	Field() string
	Func() client.IndexerFunc
}

func AddToManager(ctx context.Context, log logr.Logger, mgr manager.Manager) error {
	indexers := replicationIndexers()
	indexers = append(indexers,
		tenantowner.OwnerNameReference{},
		tenant.NamespacesReference{Obj: &capsulev1beta2.Tenant{}},
		customquota.NamespacedTargetReference{},
		customquota.NamespacedObjectUIDReference{},
		customquota.GlobalTargetReference{},
		customquota.GlobalObjectUIDReference{},
		resourcepool.NamespacesReference{Obj: &capsulev1beta2.ResourcePool{}},
		resourcepool.PoolUIDReference{Obj: &capsulev1beta2.ResourcePoolClaim{}},
		tenant.OwnerReference{},
		namespace.OwnerReference{},
		ingress.HostnamePath{Obj: &extensionsv1beta1.Ingress{}},
		ingress.HostnamePath{Obj: &networkingv1beta1.Ingress{}},
		ingress.HostnamePath{Obj: &networkingv1.Ingress{}},
	)

	return add(ctx, log, mgr, indexers)
}

// AddReplicationsToManager registers only the indexes needed by the dedicated
// replication process, without starting quota, ingress or tenant-owner caches.
func AddReplicationsToManager(ctx context.Context, log logr.Logger, mgr manager.Manager) error {
	return add(ctx, log, mgr, append(replicationIndexers(), tenant.NamespacesReference{Obj: &capsulev1beta2.Tenant{}}, tenantresource.GlobalHealthNamespaces{}, tenantresource.NamespacedHealthNamespaces{}))
}

func replicationIndexers() []CustomIndexer {
	return []CustomIndexer{
		tenantresource.GlobalServiceAccount{},
		tenantresource.GlobalProcessedItems{},
		tenantresource.FieldOwner{Obj: &capsulev1beta2.GlobalTenantResource{}},
		tenantresource.ProtectedItems{Obj: &capsulev1beta2.GlobalTenantResource{}},
		tenantresource.NamespacedServiceAccount{},
		tenantresource.NamespacedProcessedItems{},
		tenantresource.FieldOwner{Obj: &capsulev1beta2.TenantResource{}},
		tenantresource.NamespacedResourceNamespace{},
		tenantresource.ProtectedItems{Obj: &capsulev1beta2.TenantResource{}},
		tenantresource.GlobalDependencies{},
		tenantresource.NamespacedDependencies{},
		tenantresource.Triggers{Obj: &capsulev1beta2.TenantResource{}},
		tenantresource.Triggers{Obj: &capsulev1beta2.GlobalTenantResource{}},
	}
}

func add(ctx context.Context, log logr.Logger, mgr manager.Manager, indexers []CustomIndexer) error {
	for _, f := range indexers {
		if err := mgr.GetFieldIndexer().IndexField(ctx, f.Object(), f.Field(), f.Func()); err != nil {
			if utils.IsUnsupportedAPI(err) {
				log.Info(fmt.Sprintf("skipping setup of Indexer %T for object %T", f, f.Object()), "error", err.Error())

				continue
			}

			return err
		}
	}

	return nil
}
