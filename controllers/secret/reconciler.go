// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/pkg/cert"
)

func getCertificateAuthority(client client.Client, namespace, name string) (ca cert.CA, err error) {
	instance := &corev1.Secret{}

	err = client.Get(context.TODO(), types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, instance)
	if err != nil {
		return nil, fmt.Errorf("missing secret %s, cannot reconcile", name)
	}

	if instance.Data == nil {
		return nil, MissingCaError{}
	}

	ca, err = cert.NewCertificateAuthorityFromBytes(instance.Data[certSecretKey], instance.Data[privateKeySecretKey])
	if err != nil {
		return
	}

	return
}
