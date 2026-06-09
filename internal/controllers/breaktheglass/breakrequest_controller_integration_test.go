// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
)

func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	var err error
	err = capsulev1beta2.AddToScheme(scheme.Scheme)
	if err != nil {
		panic(err)
	}

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "charts", "capsule", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	cfg, err = testEnv.Start()
	if err != nil {
		panic(err)
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		panic(err)
	}

	code := m.Run()

	err = testEnv.Stop()
	if err != nil {
		panic(err)
	}

	os.Exit(code)
}

func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

func TestBreakRequestReconciler_Reconcile(t *testing.T) {
	ctx := context.Background()
	namespace := "default"

	tests := []struct {
		name         string
		resourceName string
		templateName string
	}{
		{
			name:         "should successfully reconcile the resource",
			resourceName: "test-resource-int",
			templateName: "test-template-int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nnBr := types.NamespacedName{
				Name:      tt.resourceName,
				Namespace: namespace,
			}

			// Create template
			brt := &capsulev1beta2.BreakRequestTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.templateName,
				},
				Spec: capsulev1beta2.BreakRequestTemplateSpec{
					Items: breaktheglass.TemplateItems{
						tt.templateName: {
							ManifestTemplate: mtConfigMapParameterized,
							ParamSchema:      psString,
						},
					},
				},
			}
			err := k8sClient.Create(ctx, brt)
			require.NoError(t, err)
			defer func() { _ = k8sClient.Delete(ctx, brt) }()

			// Create breakrequest
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.resourceName,
					Namespace: namespace,
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: tt.templateName,
				},
			}
			err = k8sClient.Create(ctx, br)
			require.NoError(t, err)
			defer func() { _ = k8sClient.Delete(ctx, br) }()

			r := &BreakRequestReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &record.FakeRecorder{},
				Log:      ctrl.Log,
			}

			_, err = r.Reconcile(ctx, reconcile.Request{
				NamespacedName: nnBr,
			})
			require.NoError(t, err)

			res := &capsulev1beta2.BreakRequest{}
			err = k8sClient.Get(ctx, nnBr, res)
			require.NoError(t, err)
		})
	}
}
