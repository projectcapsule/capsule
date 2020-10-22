/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
