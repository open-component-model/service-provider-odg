package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/pkg/platformservices"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/setup"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions/fluxcd"
)

var testenv env.Environment

var keepClusters = flag.Bool("keep-clusters", false, "Keep clusters alive after tests (skips teardown)")

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
			ExtraClusterPurposeMapping: []providers.ClusterPurposeMapping{
				{
					Purpose: "workload-odg",
					Profile: "kind",
					Tenancy: "Exclusive",
				},
			},
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
				Image:              fmt.Sprintf("ghcr.io/open-component-model/images/service-provider-odg:%s", version),
				LoadImageToCluster: true,
			},
		},
		PlatformServices: []platformservices.PlatformServiceSetup{
			{
				Name:                      "gateway",
				Image:                     "ghcr.io/openmcp-project/images/platform-service-gateway:v0.0.10",
				PlatformServiceConfigsDir: "platformservice-gateway",
			},
		},
		Extensions: []extensions.Extension{
			&fluxcd.FluxCD{},
		},
	}
	testenv = env.NewWithConfig(envconf.New().WithNamespace(openmcp.Namespace))
	if *keepClusters {
		testenv.Finish(func(ctx context.Context, c *envconf.Config) (context.Context, error) {
			klog.Info("--keep-clusters set: skipping teardown, keeping clusters alive")
			os.Exit(0)
			return ctx, nil
		})
	}
	openmcp.Bootstrap(testenv)
	testenv.Setup(registerAccessRequestScheme)
	os.Exit(testenv.Run(m))
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
