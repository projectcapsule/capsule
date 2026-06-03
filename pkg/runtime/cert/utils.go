// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cert

import (
	"net"
	"sort"
)

func IPsToStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))

	for _, ip := range ips {
		if ip == nil {
			continue
		}

		out = append(out, ip.String())
	}

	sort.Strings(out)

	return out
}
