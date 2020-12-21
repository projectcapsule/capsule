/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"sort"
	"strings"
)

type IngressHostnamesList []string

func (hostnames IngressHostnamesList) Len() int {
	return len(hostnames)
}

func (hostnames IngressHostnamesList) Swap(i, j int) {
	hostnames[i], hostnames[j] = hostnames[j], hostnames[i]
}

func (hostnames IngressHostnamesList) Less(i, j int) bool {
	return strings.ToLower(hostnames[i]) < strings.ToLower(hostnames[j])
}

func (hostnames IngressHostnamesList) IsStringInList(value string) (ok bool) {
	sort.Sort(hostnames)
	i := sort.SearchStrings(hostnames, value)
	ok = i < hostnames.Len() && hostnames[i] == value
	return
}
