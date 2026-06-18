// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/projectcapsule/capsule/pkg/template/functions"
)

func RenderTemplateBytes(
	context map[string]any,
	key MissingKeyOption,
	tplBytes []byte,
) ([]byte, error) {
	tmpl, err := template.New("tpl").
		Option("missingkey=" + key.String()).
		Funcs(functions.ExtraFuncMap()).
		Parse(string(tplBytes))
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, context); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	return rendered.Bytes(), nil
}

func withLineNumbers(s string) string {
	lines := strings.Split(s, "\n")

	width := len(fmt.Sprintf("%d", len(lines)))

	var b strings.Builder

	for i, line := range lines {
		fmt.Fprintf(&b, "%*d | %s\n", width, i+1, line)
	}

	return b.String()
}
