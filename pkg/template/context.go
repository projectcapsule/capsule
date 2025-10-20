package template

import (
	"encoding/json"
	"fmt"
)

// Context which results from all
// +kubebuilder:object:generate=false
type ReferenceContext map[string]interface{}

func (t *ReferenceContext) String() (string, error) {
	dataBytes, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("error marshaling TemplateContext: %w", err)
	}

	if err := json.Unmarshal(dataBytes, t); err != nil {
		return "", fmt.Errorf("error unmarshaling TemplateContext into map: %w", err)
	}

	return string(dataBytes), nil
}
