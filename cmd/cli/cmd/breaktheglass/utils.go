// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

func printBreakRequestsApprovalTable(
	br *capsulev1beta2.BreakRequest,
	app *capsulev1beta2.ApprovedProperties,
	color bool,
) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)

	t.Style().Title.Align = text.AlignCenter

	approvedDurationStr := "Unlimited"
	if app.Duration != nil && app.Duration.Duration != 0 {
		approvedDurationStr = app.Duration.Duration.String()
	}

	keepForStr := "Undefined"
	if app.KeepFor != 0 {
		keepForStr = app.KeepFor.String()
	}

	effectiveDurationStr := "Unlimited"

	switch {
	case app.Duration != nil && app.Duration.Duration != 0:
		effectiveDurationStr = app.Duration.Duration.String()
	case br.Spec.Duration != nil && br.Spec.Duration.Duration != 0:
		effectiveDurationStr = br.Spec.Duration.Duration.String()
	case br.Status.Template != nil && br.Status.Template.DefaultDuration != nil && br.Status.Template.DefaultDuration.Duration != 0:
		effectiveDurationStr = br.Status.Template.DefaultDuration.Duration.String()
	}

	t.AppendHeader(table.Row{"Field", "Value"})
	t.AppendRows([]table.Row{
		{"Name", colorizeValue(br.Name, color)},
		{"Namespace", colorizeValue(br.Namespace, color)},
		{"Duration", colorizeValue(effectiveDurationStr, color)},
		{"ApprovedDuration", colorizeValue(approvedDurationStr, color)},
		{"KeepFor", colorizeValue(keepForStr, color)},
	})

	// Example: printing .status.items nicely as YAML
	for name, item := range app.Items {
		content := prettyRawExtension(item)
		if color {
			content = colorizeYAML(content)
		}

		t.AppendSeparator()
		// Multi-line cells are supported; keep them as one cell.
		t.AppendRow(table.Row{
			fmt.Sprintf("Status Item %q", name),
			content,
		})
	}

	t.Render()
}

// PrettyRawExtension returns human-readable YAML for a RawExtension.
// - If Object is non-nil, it marshals that.
// - Else converts JSON -> YAML.
func prettyRawExtension(re *runtime.RawExtension) string {
	if re == nil {
		return "-"
	}
	// Prefer the decoded object when present.
	if re.Object != nil {
		j, err := json.Marshal(re.Object)
		if err != nil {
			return "-"
		}

		if y, errY := yaml.JSONToYAML(j); errY == nil {
			return string(y)
		}

		return string(j)
	}

	if len(re.Raw) == 0 {
		return "-"
	}

	if y, err := yaml.JSONToYAML(re.Raw); err == nil {
		return string(y)
	}

	return string(re.Raw)
}

// colorizeValue applies ANSI colors for YAML using chroma and returns a string suitable for terminal output.
func colorizeValue(src string, color bool) string {
	if !color || src == "" {
		return src
	}

	return colorize(src, chroma.Literator(chroma.Token{Type: chroma.NameTag, Value: src}))
}

// colorizeYAML applies ANSI colors for YAML using chroma and returns a string suitable for terminal output.
func colorizeYAML(src string) string {
	if src == "" {
		return src
	}

	lexer := lexers.Get("yaml")
	if lexer == nil {
		return src
	}

	it, err := lexer.Tokenise(nil, src)
	if err != nil {
		return src
	}

	return colorize(src, it)
}

func colorize(src string, it chroma.Iterator) string {
	// Choose a style; "dracula", "native", "github", etc. Fall back to "native".
	style := styles.Get("native")
	if style == nil {
		style = styles.Fallback
	}
	// Use terminal16m for truecolor; fall back to the standard terminal if not supported.
	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	var buf strings.Builder
	if err := formatter.Format(&buf, style, it); err != nil {
		return src
	}

	return buf.String()
}

func newK8sClient() (*rest.Config, ctrlclient.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, nil, err
	}

	cl, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})

	return cfg, cl, err
}

func runBreakRequestAction(
	action func(br *capsulev1beta2.BreakRequest, user *breaktheglass.AccessEntity) error,
) error {
	ctx := context.Background()

	cfg, k8sClient, err := newK8sClient()
	if err != nil {
		return err
	}

	user := &breaktheglass.AccessEntity{
		Type: breaktheglass.AccessEntityTypeUser,
		Name: cfg.Username,
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		br := &capsulev1beta2.BreakRequest{}
		if err := k8sClient.Get(
			ctx,
			ctrlclient.ObjectKey{Name: name, Namespace: namespace},
			br,
		); err != nil {
			return err
		}

		if err := action(br, user); err != nil {
			return err
		}

		return k8sClient.Status().Update(ctx, br)
	})
}
