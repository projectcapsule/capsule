// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

func ListByStatus(ctx context.Context, clt client.Client, state string) (tenantList *capsulev1beta1.TenantList, err error) {
	tenantList = &capsulev1beta1.TenantList{}

	if err = clt.List(ctx, tenantList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.state", state),
	}); err != nil {
		return
	}

	return
}

type State struct {
}

func (o State) Object() client.Object {
	return &capsulev1beta1.Tenant{}
}

func (o State) Field() string {
	return ".status.state"
}

func (o State) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		return []string{string(object.(*capsulev1beta1.Tenant).Status.State)}
	}
}