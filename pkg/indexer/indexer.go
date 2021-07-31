// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"context"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/clastix/capsule/pkg/indexer/ingress"
	"github.com/clastix/capsule/pkg/indexer/namespace"
	"github.com/clastix/capsule/pkg/indexer/tenant"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type CustomIndexer interface {
	Object() client.Object
	Field() string
	Func() client.IndexerFunc
}

func AddToManager(ctx context.Context, mgr manager.Manager) error {
	indexers := append([]CustomIndexer{},
		tenant.NamespacesReference{},
		tenant.OwnerReference{},
		namespace.OwnerReference{},
	)

	majorVer, minorVer, _, _ := utils.GetK8sVersion()
	if majorVer == 1 && minorVer < 22 {
		indexers = append(indexers,
			ingress.HostnamePath{Obj: &extensionsv1beta1.Ingress{}},
			ingress.HostnamePath{Obj: &networkingv1beta1.Ingress{}},
		)
	}
	if majorVer == 1 && minorVer >= 19 {
		indexers = append(indexers, ingress.HostnamePath{Obj: &networkingv1.Ingress{}})
	}

	for _, f := range indexers {
		if err := mgr.GetFieldIndexer().IndexField(ctx, f.Object(), f.Field(), f.Func()); err != nil {
			return err
		}
	}

	return nil
}
