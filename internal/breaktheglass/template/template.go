// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"encoding/json"
	"text/template"
)

func RenderTemplate(tplData []byte, params []byte) ([]byte, error) {
	tpl, err := ValidateTemplate(tplData)
	if err != nil {
		return nil, err
	}

	p := make(map[string]any)
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}

	var res bytes.Buffer
	if err := tpl.Execute(&res, p); err != nil {
		return nil, err
	}

	return res.Bytes(), nil
}

func ValidateTemplate(tpl []byte) (*template.Template, error) {
	return template.New("item").Option("missingkey=error").Parse(string(tpl))
}
