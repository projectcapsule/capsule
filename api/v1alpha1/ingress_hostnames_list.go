// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"sort"
)

type IngressHostnamesList []string

func (hostnames IngressHostnamesList) Len() int {
	return len(hostnames)
}

func (hostnames IngressHostnamesList) Swap(i, j int) {
	hostnames[i], hostnames[j] = hostnames[j], hostnames[i]
}

func (hostnames IngressHostnamesList) Less(i, j int) bool {
	return hostnames[i] < hostnames[j]
}

func (hostnames IngressHostnamesList) IsStringInList(value string) (ok bool) {
	sort.Sort(hostnames)
	i := sort.SearchStrings(hostnames, value)
	ok = i < hostnames.Len() && hostnames[i] == value
	return
}
