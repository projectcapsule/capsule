// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"math/big"
	"net"
	"strconv"

	corev1 "k8s.io/api/core/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
)

func serviceType(svc *corev1.Service) apirules.ServiceType {
	if svc == nil {
		return ""
	}

	switch svc.Spec.Type {
	case "", corev1.ServiceTypeClusterIP:
		return apirules.ServiceTypeClusterIP
	case corev1.ServiceTypeNodePort:
		return apirules.ServiceTypeNodePort
	case corev1.ServiceTypeLoadBalancer:
		return apirules.ServiceTypeLoadBalancer
	case corev1.ServiceTypeExternalName:
		return apirules.ServiceTypeExternalName
	default:
		return apirules.ServiceType(svc.Spec.Type)
	}
}

//nolint:exhaustive
func serviceTypeIsNodePort(svc *corev1.Service) bool {
	if svc == nil {
		return false
	}

	switch svc.Spec.Type {
	case corev1.ServiceTypeNodePort:
		return true

	case corev1.ServiceTypeLoadBalancer:
		return svc.Spec.AllocateLoadBalancerNodePorts == nil ||
			*svc.Spec.AllocateLoadBalancerNodePorts

	default:
		return false
	}
}

func cidrContainsIP(network *net.IPNet, ip net.IP) bool {
	if network == nil || ip == nil {
		return false
	}

	return network.Contains(ip)
}

func cidrContainsCIDR(parent, child *net.IPNet) bool {
	if parent == nil || child == nil {
		return false
	}

	first := child.IP
	last := lastIP(child)

	return parent.Contains(first) && parent.Contains(last)
}

func lastIP(network *net.IPNet) net.IP {
	ip := network.IP
	mask := network.Mask

	ipInt := big.NewInt(0).SetBytes(ip)
	maskInt := big.NewInt(0).SetBytes(mask)

	bits := uint(len(mask) * 8)

	allOnes := big.NewInt(0).Sub(
		big.NewInt(0).Lsh(big.NewInt(1), bits),
		big.NewInt(1),
	)

	invertedMask := big.NewInt(0).Xor(maskInt, allOnes)

	last := big.NewInt(0).Or(ipInt, invertedMask).Bytes()

	out := make(net.IP, len(ip))
	copy(out[len(out)-len(last):], last)

	return out
}

func portFromValue(value string) (int32, error) {
	port, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid nodePort value %q: %w", value, err)
	}

	return int32(port), nil
}
