// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"regexp"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:object:generate=true

type DefaultAllowedListSpec struct {
	SelectorAllowedListSpec `json:",inline"`
	Default                 string `json:"default,omitempty"`
}

func (in *DefaultAllowedListSpec) MatchDefault(value string) bool {
	return in.Default == value
}

// +kubebuilder:object:generate=true

type SelectorAllowedListSpec struct {
	AllowedListSpec      `json:",inline"`
	metav1.LabelSelector `json:",inline"`
}

func (in *SelectorAllowedListSpec) MatchSelectByName(obj client.Object) bool {
	if obj != nil {
		return in.AllowedListSpec.Match(obj.GetName()) || in.SelectorMatch(obj)
	}

	return false
}

func (in *SelectorAllowedListSpec) SelectorMatch(obj client.Object) bool {
	if obj != nil {
		selector, err := metav1.LabelSelectorAsSelector(&in.LabelSelector)
		if err != nil {
			return false
		}

		return selector.Matches(labels.Set(obj.GetLabels()))
	}

	return false
}

// +kubebuilder:object:generate=true

type AllowedListSpec struct {
	Exact []string `json:"allowed,omitempty"`
	Regex string   `json:"allowedRegex,omitempty"`
}

func (in *AllowedListSpec) Match(value string) (ok bool) {
	if in.ExactMatch(value) || in.RegexMatch(value) {
		return true
	}

	return false
}

func (in *AllowedListSpec) ExactMatch(value string) (ok bool) {
	if len(in.Exact) > 0 {
		sort.SliceStable(in.Exact, func(i, j int) bool {
			return strings.ToLower(in.Exact[i]) < strings.ToLower(in.Exact[j])
		})

		i := sort.SearchStrings(in.Exact, value)

		ok = i < len(in.Exact) && in.Exact[i] == value
	}

	return
}

func (in *AllowedListSpec) RegexMatch(value string) (ok bool) {
	if len(in.Regex) > 0 {
		ok = regexp.MustCompile(in.Regex).MatchString(value)
	}

	return
}
