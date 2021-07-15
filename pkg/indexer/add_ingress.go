// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"

	"github.com/clastix/capsule/pkg/indexer/ingress"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

func init() {
	majorVer, minorVer, _, _ := utils.GetK8sVersion()

	switch {
	case majorVer == 1 && minorVer >= 19:
		AddToIndexerFuncs = append(AddToIndexerFuncs, ingress.Hostname{Obj: &networkingv1.Ingress{}})
	case majorVer == 1 && (minorVer >= 19 && minorVer < 22):
		AddToIndexerFuncs = append(AddToIndexerFuncs, ingress.Hostname{Obj: &networkingv1beta1.Ingress{}})
	case majorVer == 1 && minorVer < 22:
		AddToIndexerFuncs = append(AddToIndexerFuncs, ingress.Hostname{Obj: &extensionsv1beta1.Ingress{}})
	}
}
