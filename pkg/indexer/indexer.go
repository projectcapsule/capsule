// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/indexer/ingress"
	"github.com/clastix/capsule/pkg/indexer/namespace"
	"github.com/clastix/capsule/pkg/indexer/tenant"
	"github.com/clastix/capsule/pkg/indexer/tenantresource"
	"github.com/clastix/capsule/pkg/utils"
)

type CustomIndexer interface {
	Object() client.Object
	Field() string
	Func() client.IndexerFunc
}

func AddToManager(ctx context.Context, log logr.Logger, mgr manager.Manager) error {
	indexers := []CustomIndexer{
		tenant.NamespacesReference{Obj: &capsulev1beta2.Tenant{}},
		tenant.OwnerReference{},
		namespace.OwnerReference{},
		ingress.HostnamePath{Obj: &extensionsv1beta1.Ingress{}},
		ingress.HostnamePath{Obj: &networkingv1beta1.Ingress{}},
		ingress.HostnamePath{Obj: &networkingv1.Ingress{}},
		tenantresource.GlobalProcessedItems{},
		tenantresource.LocalProcessedItems{},
	}

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
