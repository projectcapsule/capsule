package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AdditionalResources struct {
	NamespaceSelector metav1.LabelSelector `json:"namespaceSelector"`
	// list of k8s resources in yaml format
	Items []string `json:"items"`
}
