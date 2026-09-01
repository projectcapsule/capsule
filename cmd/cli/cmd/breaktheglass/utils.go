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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
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
	if app.KeepFor != nil && *app.KeepFor != 0 {
		keepForStr = app.KeepFor.String()
	}

	effectiveDurationStr := "Unlimited"

	switch {
	case app.Duration != nil && app.Duration.Duration != 0:
		effectiveDurationStr = app.Duration.Duration.String()
	case br.Spec.Duration != nil && br.Spec.Duration.Duration != 0:
		effectiveDurationStr = br.Spec.Duration.Duration.String()
	}

	t.AppendHeader(table.Row{"Field", "Value"})
	t.AppendRows([]table.Row{
		{"Name", colorizeValue(br.Name, color)},
		{"Namespace", colorizeValue(br.Namespace, color)},
		{"Duration", colorizeValue(effectiveDurationStr, color)},
		{"ApprovedDuration", colorizeValue(approvedDurationStr, color)},
		{"KeepFor", colorizeValue(keepForStr, color)},
	})
	t.Render()

	resources := table.NewWriter()
	resources.SetOutputMirror(os.Stdout)
	resources.SetStyle(table.StyleRounded)
	resources.AppendHeader(table.Row{"Policy", "Resource"})

	rows := 0

	for _, item := range app.Resources {
		itemRows := renderedResourceRows(item, color)
		if len(itemRows) == 0 {
			continue
		}

		if rows > 0 {
			resources.AppendSeparator()
		}

		resources.AppendRows(itemRows)
		rows += len(itemRows)
	}

	if rows > 0 {
		resources.Render()
	}
}

func renderedResourceRows(resource apiruntime.RenderedResource, color bool) []table.Row {
	policy := prettyYAML(resource.Policy)
	if color {
		policy = colorizeYAML(policy)
	}

	rows := make([]table.Row, 0, len(resource.Targets))

	for _, target := range resource.Targets {
		manifest := prettyYAML(target)
		if color {
			manifest = colorizeYAML(manifest)
		}

		rows = append(rows, table.Row{policy, manifest})
	}

	return rows
}

func prettyYAML(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "-"
	}

	if yamlData, yamlErr := yaml.JSONToYAML(data); yamlErr == nil {
		return string(yamlData)
	}

	return string(data)
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

	if err := impersonation.applyTo(cfg); err != nil {
		return nil, nil, err
	}

	cl, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})

	return cfg, cl, err
}

type impersonationOptions struct {
	User   string
	Groups []string
}

func (o impersonationOptions) applyTo(cfg *rest.Config) error {
	if o.User != "" {
		cfg.Impersonate.UserName = o.User
		cfg.Impersonate.Groups = append([]string(nil), o.Groups...)
	} else if len(o.Groups) > 0 {
		cfg.Impersonate.Groups = append([]string(nil), o.Groups...)
	}

	if len(cfg.Impersonate.Groups) > 0 && cfg.Impersonate.UserName == "" {
		return fmt.Errorf("--as-group requires --as or an impersonated user in the kubeconfig")
	}

	return nil
}

func runBreakRequestAction(phase capsulev1beta2.RequestPhase) error {
	ctx := context.Background()

	_, k8sClient, err := newK8sClient()
	if err != nil {
		return err
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

		return patchBreakRequestStatus(ctx, k8sClient, br, func() error {
			br.Status.Phase = phase

			return nil
		})
	})
}

func patchBreakRequestStatus(
	ctx context.Context,
	k8sClient ctrlclient.Client,
	br *capsulev1beta2.BreakRequest,
	action func() error,
) error {
	before := br.DeepCopy()

	if err := action(); err != nil {
		return err
	}

	return k8sClient.Status().Patch(
		ctx,
		br,
		ctrlclient.MergeFromWithOptions(before, ctrlclient.MergeFromWithOptimisticLock{}),
	)
}
