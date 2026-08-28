package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/setup"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	initLogging()
	version := mustVersion()
	openmcp := setup.OpenMCPSetup{
		Namespace: "openmcp-system",
		Operator: setup.OpenMCPOperatorSetup{
			Name: "openmcp-operator",
			// renovate: datasource=docker depName=ghcr.io/openmcp-project/images/openmcp-operator
			Image:        "ghcr.io/openmcp-project/images/openmcp-operator:v1.3.0",
			Environment:  "debug",
			PlatformName: "platform",
		},
		ClusterProviders: []providers.ClusterProviderSetup{
			{
				Name: "kind",
				// renovate: datasource=docker depName=ghcr.io/openmcp-project/images/cluster-provider-kind
				Image: "ghcr.io/openmcp-project/images/cluster-provider-kind:v0.6.0",
			},
		},
		ServiceProviders: []providers.ServiceProviderSetup{
			{
				Name:               "odg",
				Image:              fmt.Sprintf("ghcr.io/openmcp-project/images/service-provider-odg:%s", version),
				LoadImageToCluster: true,
			},
		},
	}
	testenv = env.NewWithConfig(envconf.New().WithNamespace(openmcp.Namespace))
	openmcp.Bootstrap(testenv)
	testenv.Setup(installFlux, registerFluxSchemes, registerAccessRequestScheme)
	os.Exit(testenv.Run(m))
}

func installFlux(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	// Use flux binary from bin directory if available, otherwise fall back to PATH
	fluxBinary := "../../bin/flux"
	if _, err := os.Stat(fluxBinary); os.IsNotExist(err) {
		fluxBinary = "flux" // Fall back to PATH
	}

	args := []string{"install"}
	if kubeconfig := cfg.KubeconfigFile(); kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	out, err := exec.Command(fluxBinary, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("flux install failed: %w: %s", err, string(out))
	}
	klog.Infof("flux install output: %s", string(out))
	return ctx, nil
}

func registerFluxSchemes(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	scheme := cfg.Client().Resources().GetScheme()
	if err := helmv2.AddToScheme(scheme); err != nil {
		return ctx, fmt.Errorf("failed to register helm-controller scheme: %w", err)
	}
	if err := sourcev1.AddToScheme(scheme); err != nil {
		return ctx, fmt.Errorf("failed to register source-controller scheme: %w", err)
	}
	return ctx, nil
}

func registerAccessRequestScheme(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	scheme := cfg.Client().Resources().GetScheme()
	if err := clustersv1alpha1.AddToScheme(scheme); err != nil {
		return ctx, fmt.Errorf("failed to register clusters scheme: %w", err)
	}
	return ctx, nil
}

func mustVersion() string {
	cmd := exec.Command("../../hack/common/get-version.sh")
	version, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(version))
}

func initLogging() {
	klog.InitFlags(nil)
	if err := flag.Set("v", "2"); err != nil {
		panic(err)
	}
	flag.Parse()
}
