package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// Additional Context to enhance templating
// +kubebuilder:object:generate=true
type TemplateContext struct {
	Resources []*ResourceReference `json:"resources,omitempty"`
}

func (t *TemplateContext) GatherContext(
	ctx context.Context,
	kubeClient client.Client,
	data map[string]interface{},
	namespace string,
) (context tpl.ReferenceContext, errors []error) {
	context = tpl.ReferenceContext{}

	// Template Context for Tenant
	if len(data) != 0 {
		if err := t.selfTemplate(data); err != nil {
			return context, []error{fmt.Errorf("cloud not template: %w", err)}
		}
	}

	// Load external Resources
	for index, resource := range t.Resources {
		val, err := resource.LoadResources(ctx, kubeClient, namespace)
		if err != nil {
			errors = append(errors, err)

			continue
		}

		if len(val) > 0 {
			resourceIndex := resource.Index
			if resourceIndex == "" {
				resourceIndex = string(index)
			}

			context[resource.Index] = val
		}
	}

	return
}

// Templates itself with the option to populate tenant fields
// this can be useful if you have per tenant items, that you want to interact with
func (t *TemplateContext) selfTemplate(
	data map[string]interface{},
) (err error) {
	dataBytes, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("error marshaling TemplateContext: %w", err)
	}

	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return fmt.Errorf("error unmarshaling TemplateContext into map: %w", err)
	}

	tmpl, err := template.New("tpl").Option("missingkey=error").Funcs(tpl.ExtraFuncMap()).Parse(string(dataBytes))
	if err != nil {
		return fmt.Errorf("error parsing template: %w", err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	tplContext := &TemplateContext{}
	if err := json.Unmarshal(rendered.Bytes(), tplContext); err != nil {
		return fmt.Errorf("error unmarshaling JSON into TemplateContext: %w", err)
	}

	// Reassing templated context
	*t = *tplContext

	return nil
}

// +kubebuilder:object:generate=true
type ResourceReference struct {
	// Index where the results are published in the templating/CEL
	Index string `json:"index,omitempty"`
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind" protobuf:"bytes,1,opt,name=kind"`
	// API version of the referent.
	APIVersion string `json:"apiVersion" protobuf:"bytes,5,opt,name=apiVersion"`
	// Name of the values referent. This is useful
	// when you traying to get a specific resource
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Name string `json:"name,omitempty"`
	// Namespace of the values referent.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Selector which allows to get any amount of these resources based on labels
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
	// Only relevant if name is set. If an item is not optional, there will be an error thrown when it does not exist
	// +kubebuilder:default:=true
	Optional bool `json:"optional,omitempty"`
}

func (t ResourceReference) LoadResources(
	ctx context.Context,
	kubeClient client.Client,
	namespace string,
) ([]unstructured.Unstructured, error) {
	if namespace != "" {
		t.Namespace = namespace
	}

	// For a single item we are not using list
	if t.Name != "" {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(t.APIVersion)
		obj.SetKind(t.Kind)

		key := client.ObjectKey{
			Name:      t.Name,
			Namespace: t.Namespace,
		}

		if err := kubeClient.Get(ctx, key, obj); err != nil {
			return nil, fmt.Errorf("failed to get %s/%s: %w", t.Kind, t.Name, err)
		}

		return []unstructured.Unstructured{*obj}, nil
	}

	list := &unstructured.UnstructuredList{}
	list.SetAPIVersion(t.APIVersion)
	list.SetKind(t.Kind + "List")

	// Prepare list options.
	var opts []client.ListOption
	if t.Namespace != "" {
		opts = append(opts, client.InNamespace(t.Namespace))
	}

	if t.Selector != nil {
		selector, err := metav1.LabelSelectorAsSelector(t.Selector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}

		opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
	}

	// List the resources.
	if err := kubeClient.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("failed to list: %w", err)
	}

	// Prepare a result map. For example, mapping resource name to its UID.
	results := []unstructured.Unstructured{}
	for _, item := range list.Items {
		results = append(results, item)
	}

	return results, nil

}
