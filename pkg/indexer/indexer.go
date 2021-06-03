// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type CustomIndexer interface {
	Object() client.Object
	Field() string
	Func() client.IndexerFunc
}

var AddToIndexerFuncs []CustomIndexer

func AddToManager(m manager.Manager) error {
	for _, f := range AddToIndexerFuncs {
		if err := m.GetFieldIndexer().IndexField(context.TODO(), f.Object(), f.Field(), f.Func()); err != nil {
			return err
		}
	}
	return nil
}
