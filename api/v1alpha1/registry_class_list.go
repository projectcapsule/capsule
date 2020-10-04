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

type RegistryList []string

func (in RegistryList) Len() int {
	return len(in)
}

func (in RegistryList) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}

func (in RegistryList) Less(i, j int) bool {
	return strings.ToLower(in[i]) < strings.ToLower(in[j])
}

func (in RegistryList) IsStringInList(value string) (ok bool) {
	sort.Sort(in)
	i := sort.SearchStrings(in, value)
	ok = i < in.Len() && in[i] == value
	return
}
