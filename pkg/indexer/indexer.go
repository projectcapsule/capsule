// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"context"

	"github.com/clastix/capsule/pkg/indexer/ingress"
	"github.com/clastix/capsule/pkg/indexer/namespace"
	"github.com/clastix/capsule/pkg/indexer/tenant"
	"github.com/clastix/capsule/pkg/webhook/utils"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type CustomIndexer interface {
	Object() client.Object
	Field() string
	Func() client.IndexerFunc
}

func AddToManager(m manager.Manager) error {
	indexers := append([]CustomIndexer{},
		tenant.IngressHostnames{},
		tenant.NamespacesReference{},
		tenant.OwnerReference{},
		namespace.OwnerReference{},
		ingress.Hostname{Obj: &extensionsv1beta1.Ingress{}},
		ingress.Hostname{Obj: &networkingv1beta1.Ingress{}},
	)

	majorVer, minorVer, _, _ := utils.GetK8sVersion()
	if majorVer == 1 && minorVer >= 19 {
		indexers = append(indexers, ingress.Hostname{Obj: &networkingv1.Ingress{}})
	}

	for _, f := range indexers {
		if err := m.GetFieldIndexer().IndexField(context.TODO(), f.Object(), f.Field(), f.Func()); err != nil {
			return err
		}
	}

	return nil
}
