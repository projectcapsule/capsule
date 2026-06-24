// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"net"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestServiceType(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want apirules.ServiceType
	}{
		{
			name: "nil service",
			svc:  nil,
			want: "",
		},
		{
			name: "empty service type is treated as ClusterIP",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{},
			},
			want: apirules.ServiceTypeClusterIP,
		},
		{
			name: "ClusterIP",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
			},
			want: apirules.ServiceTypeClusterIP,
		},
		{
			name: "NodePort",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeNodePort,
				},
			},
			want: apirules.ServiceTypeNodePort,
		},
		{
			name: "LoadBalancer",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
			want: apirules.ServiceTypeLoadBalancer,
		},
		{
			name: "ExternalName",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeExternalName,
				},
			},
			want: apirules.ServiceTypeExternalName,
		},
		{
			name: "unknown type is preserved",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceType("CustomType"),
				},
			},
			want: apirules.ServiceType("CustomType"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serviceType(tt.svc)
			if got != tt.want {
				t.Fatalf("serviceType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceTypeIsNodePort(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{
		{
			name: "nil service",
			svc:  nil,
			want: false,
		},
		{
			name: "ClusterIP",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
			},
			want: false,
		},
		{
			name: "ExternalName",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeExternalName,
				},
			},
			want: false,
		},
		{
			name: "NodePort",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeNodePort,
				},
			},
			want: true,
		},
		{
			name: "LoadBalancer allocation default",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
			want: true,
		},
		{
			name: "LoadBalancer allocation explicitly enabled",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:                          corev1.ServiceTypeLoadBalancer,
					AllocateLoadBalancerNodePorts: &enabled,
				},
			},
			want: true,
		},
		{
			name: "LoadBalancer allocation explicitly disabled",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:                          corev1.ServiceTypeLoadBalancer,
					AllocateLoadBalancerNodePorts: &disabled,
				},
			},
			want: false,
		},
		{
			name: "unknown service type",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceType("CustomType"),
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serviceTypeIsNodePort(tt.svc)
			if got != tt.want {
				t.Fatalf("serviceTypeIsNodePort() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCIDRContainsIP(t *testing.T) {
	_, allowedIPv4, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, allowedIPv6, err := net.ParseCIDR("2001:db8::/32")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	tests := []struct {
		name    string
		network *net.IPNet
		ip      net.IP
		want    bool
	}{
		{
			name:    "nil network",
			network: nil,
			ip:      net.ParseIP("10.0.0.2"),
			want:    false,
		},
		{
			name:    "nil IP",
			network: allowedIPv4,
			ip:      nil,
			want:    false,
		},
		{
			name:    "IPv4 contains IP",
			network: allowedIPv4,
			ip:      net.ParseIP("10.0.0.2"),
			want:    true,
		},
		{
			name:    "IPv4 does not contain IP",
			network: allowedIPv4,
			ip:      net.ParseIP("192.168.0.1"),
			want:    false,
		},
		{
			name:    "IPv4 network does not contain IPv6 IP",
			network: allowedIPv4,
			ip:      net.ParseIP("2001:db8::1"),
			want:    false,
		},
		{
			name:    "IPv6 contains IP",
			network: allowedIPv6,
			ip:      net.ParseIP("2001:db8::1"),
			want:    true,
		},
		{
			name:    "IPv6 does not contain IP",
			network: allowedIPv6,
			ip:      net.ParseIP("2001:db9::1"),
			want:    false,
		},
		{
			name:    "IPv6 network does not contain IPv4 IP",
			network: allowedIPv6,
			ip:      net.ParseIP("10.0.0.2"),
			want:    false,
		},
		{
			name:    "invalid parsed IP",
			network: allowedIPv4,
			ip:      net.ParseIP("not-an-ip"),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cidrContainsIP(tt.network, tt.ip)
			if got != tt.want {
				t.Fatalf("cidrContainsIP() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCIDRContainsCIDR(t *testing.T) {
	_, allowedIPv4, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, childIPv4Inside, err := net.ParseCIDR("10.0.1.0/24")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, childIPv4Exact, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, childIPv4PartialOutside, err := net.ParseCIDR("10.0.0.0/7")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, childIPv4Outside, err := net.ParseCIDR("192.168.0.0/16")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, allowedIPv6, err := net.ParseCIDR("2001:db8::/32")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, childIPv6Inside, err := net.ParseCIDR("2001:db8:1::/48")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, childIPv6Exact, err := net.ParseCIDR("2001:db8::/32")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	_, childIPv6Outside, err := net.ParseCIDR("2001:db9::/32")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	tests := []struct {
		name   string
		parent *net.IPNet
		child  *net.IPNet
		want   bool
	}{
		{
			name:   "nil parent",
			parent: nil,
			child:  childIPv4Inside,
			want:   false,
		},
		{
			name:   "nil child",
			parent: allowedIPv4,
			child:  nil,
			want:   false,
		},
		{
			name:   "IPv4 parent contains child",
			parent: allowedIPv4,
			child:  childIPv4Inside,
			want:   true,
		},
		{
			name:   "IPv4 parent contains exact child",
			parent: allowedIPv4,
			child:  childIPv4Exact,
			want:   true,
		},
		{
			name:   "IPv4 parent does not fully contain wider child",
			parent: allowedIPv4,
			child:  childIPv4PartialOutside,
			want:   false,
		},
		{
			name:   "IPv4 parent does not contain outside child",
			parent: allowedIPv4,
			child:  childIPv4Outside,
			want:   false,
		},
		{
			name:   "IPv4 parent does not contain IPv6 child",
			parent: allowedIPv4,
			child:  childIPv6Inside,
			want:   false,
		},
		{
			name:   "IPv6 parent contains child",
			parent: allowedIPv6,
			child:  childIPv6Inside,
			want:   true,
		},
		{
			name:   "IPv6 parent contains exact child",
			parent: allowedIPv6,
			child:  childIPv6Exact,
			want:   true,
		},
		{
			name:   "IPv6 parent does not contain outside child",
			parent: allowedIPv6,
			child:  childIPv6Outside,
			want:   false,
		},
		{
			name:   "IPv6 parent does not contain IPv4 child",
			parent: allowedIPv6,
			child:  childIPv4Inside,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cidrContainsCIDR(tt.parent, tt.child)
			if got != tt.want {
				t.Fatalf("cidrContainsCIDR() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLastIP(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want string
	}{
		{
			name: "IPv4 /24",
			cidr: "10.0.1.0/24",
			want: "10.0.1.255",
		},
		{
			name: "IPv4 /32",
			cidr: "10.0.1.44/32",
			want: "10.0.1.44",
		},
		{
			name: "IPv4 /8",
			cidr: "10.0.0.0/8",
			want: "10.255.255.255",
		},
		{
			name: "IPv6 /32",
			cidr: "2001:db8::/32",
			want: "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff",
		},
		{
			name: "IPv6 /128",
			cidr: "2001:db8::2/128",
			want: "2001:db8::2",
		},
		{
			name: "IPv6 /48",
			cidr: "2001:db8:1::/48",
			want: "2001:db8:1:ffff:ffff:ffff:ffff:ffff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, network, err := net.ParseCIDR(tt.cidr)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			got := lastIP(network)

			if got.String() != tt.want {
				t.Fatalf("lastIP(%q) = %q, want %q", tt.cidr, got.String(), tt.want)
			}
		})
	}
}

func TestPortFromValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int32
		wantErr string
	}{
		{
			name:  "valid port",
			value: "30080",
			want:  30080,
		},
		{
			name:  "zero is parsed",
			value: "0",
			want:  0,
		},
		{
			name:  "negative is parsed",
			value: "-1",
			want:  -1,
		},
		{
			name:  "max int32 is parsed",
			value: "2147483647",
			want:  2147483647,
		},
		{
			name:    "above int32 returns error",
			value:   "2147483648",
			wantErr: `invalid nodePort value "2147483648"`,
		},
		{
			name:    "empty returns error",
			value:   "",
			wantErr: `invalid nodePort value ""`,
		},
		{
			name:    "non numeric returns error",
			value:   "not-a-port",
			wantErr: `invalid nodePort value "not-a-port"`,
		},
		{
			name:    "decimal returns error",
			value:   "30080.5",
			wantErr: `invalid nodePort value "30080.5"`,
		},
		{
			name:    "whitespace returns error",
			value:   " 30080 ",
			wantErr: `invalid nodePort value " 30080 "`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := portFromValue(tt.value)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != tt.want {
				t.Fatalf("portFromValue() = %d, want %d", got, tt.want)
			}
		})
	}
}
