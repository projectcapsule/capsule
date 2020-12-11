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
	"regexp"
	"sort"
	"strings"
)

type IngressHostnamesList []string

func (n IngressHostnamesList) Len() int {
	return len(n)
}

func (n IngressHostnamesList) Swap(i, j int) {
	n[i], n[j] = n[j], n[i]
}

func (n IngressHostnamesList) Less(i, j int) bool {
	return strings.ToLower(n[i]) < strings.ToLower(n[j])
}

func (n IngressHostnamesList) IsStringInList(value string) (ok bool) {

	sort.Sort(n)
	i := sort.SearchStrings(n, value)
	ok = i < n.Len() && n[i] == value
	return
}

func (n IngressHostnamesList) AreStringsInList(values []string) bool {

	sort.Sort(n)

	for _, value := range values {

		index := sort.SearchStrings(n, value)
		isOk := index < n.Len() && n[index] == value
		if isOk == false {
			return false
		}
	}
	return true
}

type IngressRegex string

func (ir IngressRegex) MatchesAllStrings(values []string) bool {

	for _, value := range values {

		matched, _ := regexp.MatchString(string(ir), value)
		if matched == false {
			return false
		}
	}
	return true
}
