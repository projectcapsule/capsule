// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

func TenantByStatusNamespace(ctx context.Context, c client.Client, namespace string) (*capsulev1beta2.Tenant, error) {
	tntList := &capsulev1beta2.TenantList{}
	tnt := &capsulev1beta2.Tenant{}

	if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", namespace),
	}); err != nil {
		return nil, err
	}

	if len(tntList.Items) == 0 {
		return tnt, nil
	}

	*tnt = tntList.Items[0]

	return tnt, nil
}
