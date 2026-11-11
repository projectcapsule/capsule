// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

// LoadTriggerDeliveryKey runs before webhook registration on every replica.
// Create races converge on one immutable Secret; no key is held in API status.
func LoadTriggerDeliveryKey(ctx context.Context, c client.Client, namespace string, owner *capsulev1beta2.CapsuleConfiguration) (*replication.DeliveryKey, error) {
	if namespace == "" || owner == nil || owner.UID == "" {
		return nil, fmt.Errorf("trigger delivery requires a controller namespace and persisted CapsuleConfiguration")
	}

	identity := sha256.Sum256([]byte(owner.Name))
	key := client.ObjectKey{Namespace: namespace, Name: fmt.Sprintf("capsule-trigger-delivery-%x", identity[:8])}

	secret := &corev1.Secret{}

	err := c.Get(ctx, key, secret)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("read trigger delivery secret: %w", err)
	}

	if apierrors.IsNotFound(err) {
		if err := createTriggerDeliverySecret(ctx, c, key, owner); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		// Always read the persisted winner, including after a concurrent creation.
		if err := c.Get(ctx, key, secret); err != nil {
			return nil, fmt.Errorf("read created trigger delivery secret: %w", err)
		}
	}

	owned := false
	for _, ref := range secret.OwnerReferences {
		owned = owned || (ref.APIVersion == capsulev1beta2.GroupVersion.String() && ref.Kind == "CapsuleConfiguration" && ref.Name == owner.Name && ref.UID == owner.UID)
	}

	if !owned || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque {
		return nil, fmt.Errorf("trigger delivery secret must be immutable, Opaque and owned by the current CapsuleConfiguration")
	}

	return replication.NewDeliveryKey(secret.Data["key"])
}

func createTriggerDeliverySecret(ctx context.Context, c client.Client, key client.ObjectKey, owner *capsulev1beta2.CapsuleConfiguration) error {
	secret := &corev1.Secret{}
	secret.Name, secret.Namespace = key.Name, key.Namespace
	secret.Type = corev1.SecretTypeOpaque
	secret.Immutable = new(true)
	secret.Labels = map[string]string{meta.CreatedByCapsuleLabel: meta.ValueController}

	secret.Data = map[string][]byte{"key": make([]byte, 32)}
	if _, err := rand.Read(secret.Data["key"]); err != nil {
		return fmt.Errorf("generate trigger delivery key: %w", err)
	}

	if err := controllerutil.SetOwnerReference(owner, secret, c.Scheme()); err != nil {
		return fmt.Errorf("own trigger delivery secret: %w", err)
	}

	if err := c.Create(ctx, secret); err != nil {
		return fmt.Errorf("create trigger delivery secret: %w", err)
	}

	return nil
}
