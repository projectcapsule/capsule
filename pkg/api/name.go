package api

// Name must be unique within a namespace. Is required when creating resources, although
// some resources may allow a client to request the generation of an appropriate name
// automatically. Name is primarily intended for creation idempotence and configuration
// definition.
// Cannot be updated.
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names#names
// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
// +kubebuilder:validation:MaxLength=20
// +kubebuilder:object:generate=true
type Name string

func (n Name) String() string {
	return string(n)
}
