// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/clastix/capsule/pkg/cert"
)

func getCertificateAuthority(client client.Client, namespace string) (ca cert.CA, err error) {
	instance := &corev1.Secret{}

	err = client.Get(context.TODO(), types.NamespacedName{
		Namespace: namespace,
		Name:      caSecretName,
	}, instance)
	if err != nil {
		return nil, fmt.Errorf("missing secret %s, cannot reconcile", caSecretName)
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

func forOptionPerInstanceName(instanceName string) builder.ForOption {
	return builder.WithPredicates(predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) bool {
			return filterByName(event.Object.GetName(), instanceName)
		},
		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			return filterByName(deleteEvent.Object.GetName(), instanceName)
		},
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return filterByName(updateEvent.ObjectNew.GetName(), instanceName)
		},
		GenericFunc: func(genericEvent event.GenericEvent) bool {
			return filterByName(genericEvent.Object.GetName(), instanceName)
		},
	})
}

func filterByName(objName, desired string) bool {
	return objName == desired
}
