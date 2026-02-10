// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func fetchCACertFromSecret(ctx context.Context, k8sClient client.Client, namespace, secretName, secretCaKey string) ([]byte, error) {
	var secret corev1.Secret
	key := client.ObjectKey{Namespace: namespace, Name: secretName}

	if err := k8sClient.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("unable to fetch CA secret %s/%s: %w", namespace, secretName, err)
	}

	data, ok := secret.Data[secretCaKey]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s does not contain key '%s'", namespace, secretName, secretCaKey)
	}

	return data, nil
}
