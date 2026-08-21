package e2e

// DISABLED: Imports commented out because TestServiceProviderDeployment is disabled
// due to missing Gateway API CRDs. Uncomment when the test is re-enabled.
/*
import (
	"context"
	"testing"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)
*/

// TestServiceProviderDeployment validates that the delivery-dashboard chart
// actually deploys successfully to the workload cluster using publicly available
// images (no pull secrets required). The chart at europe-docker.pkg.dev is publicly
// accessible.
//
// This test complements TestServiceProvider which validates Flux resource creation
// with pull secret configuration using dummy credentials.
//
// DISABLED: This test is currently commented out because the delivery-dashboard chart
// requires Gateway API CRDs (HTTPRoute from gateway.networking.k8s.io/v1) to be installed
// on the workload cluster. The chart includes HTTPRoute resources in templates/service.yaml
// but the CRDs are not installed by default.
//
// To enable this test, we need to:
// 1. Add Gateway API CRD installation as a prerequisite (either via Flux HelmRelease dependencies
//    or as part of the test cluster setup)
// 2. Install the Gateway API CRDs chart: oci://ghcr.io/kubernetes-sigs/gateway-api/gateway-api:v1.2.0
//    OR the full Envoy Gateway controller: oci://docker.io/envoyproxy/gateway-helm:v1.2.1
//
// See: https://gateway-api.sigs.k8s.io/ for Gateway API documentation
/*
func TestServiceProviderDeployment(t *testing.T) {
	var onboardingList unstructured.UnstructuredList
	var tenantNamespace string

	deploymentTest := features.New("provider deployment test").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			// Use platform-public configuration without pull secrets
			if _, err := resources.CreateObjectsFromDir(ctx, c, "platform-public"); err != nil {
				t.Errorf("failed to create platform cluster objects: %v", err)
			}
			return ctx
		}).
		Setup(providers.CreateMCP("test-mcp-public")).
		Assess("deploy and verify ODG on workload cluster",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				onboardingConfig, err := clusterutils.OnboardingConfig()
				if err != nil {
					t.Error(err)
					return ctx
				}
				objList, err := resources.CreateObjectsFromDir(ctx, onboardingConfig, "onboarding-public")
				if err != nil {
					t.Errorf("failed to create onboarding cluster objects: %v", err)
					return ctx
				}
				objList.DeepCopyInto(&onboardingList)

				// Calculate tenant namespace
				tenantNamespace, err = getTenantNamespace("test-mcp-public", objList.Items[0].GetNamespace())
				if err != nil {
					t.Errorf("failed to calculate tenant namespace: %v", err)
					return ctx
				}
				t.Logf("Calculated tenant namespace: %s", tenantNamespace)

				// Wait for OCIRepository to become Ready
				// Chart is publicly accessible, so this should succeed
				ociRepo := &sourcev1.OCIRepository{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "odg-dashboard",
						Namespace: tenantNamespace,
					},
				}

				t.Logf("Waiting for OCIRepository to become Ready...")
				if err := wait.For(
					func(ctx context.Context) (bool, error) {
						if err := c.Client().Resources().Get(ctx, "odg-dashboard", tenantNamespace, ociRepo); err != nil {
							return false, nil
						}
						return apimeta.IsStatusConditionTrue(ociRepo.Status.Conditions, "Ready"), nil
					},
					wait.WithTimeout(3*time.Minute),
					wait.WithInterval(10*time.Second),
				); err != nil {
					t.Errorf("OCIRepository did not become Ready: %v", err)
					return ctx
				}
				t.Logf("OCIRepository is Ready")

				// Wait for HelmRelease to become Ready
				// This validates the chart actually deploys to the workload cluster
				helmRelease := &helmv2.HelmRelease{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "odg",
						Namespace: tenantNamespace,
					},
				}

				t.Logf("Waiting for HelmRelease to become Ready...")
				if err := wait.For(
					func(ctx context.Context) (bool, error) {
						if err := c.Client().Resources().Get(ctx, "odg", tenantNamespace, helmRelease); err != nil {
							return false, nil
						}
						ready := apimeta.IsStatusConditionTrue(helmRelease.Status.Conditions, "Ready")
						if !ready {
							// Log current status for debugging
							for _, cond := range helmRelease.Status.Conditions {
								if cond.Type == "Ready" {
									t.Logf("HelmRelease Ready condition: %s - %s", cond.Status, cond.Message)
								}
							}
						}
						return ready, nil
					},
					wait.WithTimeout(8*time.Minute),
					wait.WithInterval(15*time.Second),
				); err != nil {
					t.Errorf("HelmRelease did not become Ready: %v", err)
					return ctx
				}
				t.Logf("HelmRelease is Ready")

				// Verify deployment on workload cluster
				workloadConfig, err := getWorkloadClusterClient(ctx, c, tenantNamespace, "test-mcp-public")
				if err != nil {
					t.Errorf("failed to get workload cluster client: %v", err)
					return ctx
				}

				workloadClient, err := client.New(workloadConfig, client.Options{
					Scheme: c.Client().Resources().GetScheme(),
				})
				if err != nil {
					t.Errorf("failed to create workload client: %v", err)
					return ctx
				}

				// Verify odg-system namespace exists
				namespace := &corev1.Namespace{}
				if err := workloadClient.Get(ctx, client.ObjectKey{Name: "odg-system"}, namespace); err != nil {
					t.Errorf("odg-system namespace does not exist on workload cluster: %v", err)
					return ctx
				}
				t.Logf("odg-system namespace exists on workload cluster")

				// Wait for at least one pod to be Running
				t.Logf("Waiting for pods to be Running in odg-system...")
				err = wait.For(
					func(ctx context.Context) (bool, error) {
						podList := &corev1.PodList{}
						if err := workloadClient.List(ctx, podList, client.InNamespace("odg-system")); err != nil {
							return false, nil
						}
						for _, pod := range podList.Items {
							if pod.Status.Phase == corev1.PodRunning {
								return true, nil
							}
						}
						return false, nil
					},
					wait.WithTimeout(5*time.Minute),
					wait.WithInterval(5*time.Second),
				)
				if err != nil {
					t.Errorf("no running pods found in odg-system namespace: %v", err)
					return ctx
				}

				// Count running pods
				podList := &corev1.PodList{}
				if err := workloadClient.List(ctx, podList, client.InNamespace("odg-system")); err != nil {
					t.Errorf("failed to list pods: %v", err)
					return ctx
				}

				runningPods := 0
				for _, pod := range podList.Items {
					if pod.Status.Phase == corev1.PodRunning {
						runningPods++
						t.Logf("Running pod: %s", pod.Name)
					}
				}

				t.Logf("✅ Deployment successful: %d running pod(s) in odg-system on workload cluster", runningPods)
				return ctx
			},
		).
		Assess("verify domain objects can be created", providers.ImportDomainAPIs("test-mcp-public", "mcp")).
		Teardown(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			onboardingConfig, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Error(err)
				return ctx
			}
			for _, obj := range onboardingList.Items {
				if err := resources.DeleteObject(ctx, onboardingConfig, &obj, wait.WithTimeout(time.Minute)); err != nil {
					t.Errorf("failed to delete onboarding object: %v", err)
				}
			}
			return ctx
		}).
		Teardown(providers.DeleteMCP("test-mcp-public", wait.WithTimeout(5*time.Minute)))
	testenv.Test(t, deploymentTest.Feature())
}
*/
