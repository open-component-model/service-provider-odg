package e2e

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
	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

func TestServiceProvider(t *testing.T) {
	var onboardingList unstructured.UnstructuredList
	var tenantNamespace string

	basicProviderTest := features.New("provider test").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := resources.CreateObjectsFromDir(ctx, c, "platform"); err != nil {
				t.Errorf("failed to create platform cluster objects: %v", err)
			}
			return ctx
		}).
		Setup(providers.CreateMCP("test-mcp")).
		Assess("verify service can be successfully consumed",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				onboardingConfig, err := clusterutils.OnboardingConfig()
				if err != nil {
					t.Error(err)
					return ctx
				}
				objList, err := resources.CreateObjectsFromDir(ctx, onboardingConfig, "onboarding")
				if err != nil {
					t.Errorf("failed to create onboarding cluster objects: %v", err)
					return ctx
				}
				for _, obj := range objList.Items {
					if err := wait.For(conditions.Match(&obj, onboardingConfig, "Ready", corev1.ConditionTrue)); err != nil {
						t.Error(err)
					}
				}
				objList.DeepCopyInto(&onboardingList)

				// Calculate tenant namespace for subsequent checks
				tenantNamespace, err = getTenantNamespace("test-mcp", objList.Items[0].GetNamespace())
				if err != nil {
					t.Errorf("failed to calculate tenant namespace: %v", err)
					return ctx
				}
				t.Logf("Calculated tenant namespace: %s", tenantNamespace)

				return ctx
			},
		).
		Assess("verify OCIRepository is created correctly",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				ociRepo := &sourcev1.OCIRepository{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "odg-dashboard",
						Namespace: tenantNamespace,
					},
				}

				// Wait for OCIRepository to become Ready (3 minutes for chart pull)
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
					t.Errorf("OCIRepository did not become ready: %v", err)
					return ctx
				}

				// Validate spec matches ProviderConfig
				expectedURL := "oci://europe-docker.pkg.dev/gardener-project/releases/charts/odg/delivery-dashboard"
				if ociRepo.Spec.URL != expectedURL {
					t.Errorf("OCIRepository URL mismatch: got %q, want %q", ociRepo.Spec.URL, expectedURL)
				}

				if ociRepo.Spec.Reference == nil || ociRepo.Spec.Reference.Tag != "0.439.0" {
					t.Errorf("OCIRepository version mismatch")
				}

				if ociRepo.Spec.SecretRef == nil || ociRepo.Spec.SecretRef.Name != "privateregcred" {
					t.Errorf("OCIRepository secretRef mismatch")
				}

				t.Logf("OCIRepository validation passed")
				return ctx
			},
		).
		Assess("verify HelmRelease is created correctly",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				helmRelease := &helmv2.HelmRelease{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "odg",
						Namespace: tenantNamespace,
					},
				}

				// Wait for HelmRelease to become Ready (5 minutes for Helm install)
				if err := wait.For(
					func(ctx context.Context) (bool, error) {
						if err := c.Client().Resources().Get(ctx, "odg", tenantNamespace, helmRelease); err != nil {
							return false, nil
						}
						return apimeta.IsStatusConditionTrue(helmRelease.Status.Conditions, "Ready"), nil
					},
					wait.WithTimeout(5*time.Minute),
					wait.WithInterval(15*time.Second),
				); err != nil {
					t.Errorf("HelmRelease did not become ready: %v", err)
					return ctx
				}

				// Validate remote cluster deployment configuration
				if helmRelease.Spec.TargetNamespace != "odg-system" {
					t.Errorf("HelmRelease targetNamespace mismatch: got %q, want %q",
						helmRelease.Spec.TargetNamespace, "odg-system")
				}

				if helmRelease.Spec.ChartRef == nil || helmRelease.Spec.ChartRef.Name != "odg-dashboard" {
					t.Errorf("HelmRelease chartRef mismatch")
				}

				if helmRelease.Spec.KubeConfig == nil {
					t.Errorf("HelmRelease should have KubeConfig configured for remote deployment")
				}

				t.Logf("HelmRelease validation passed")
				return ctx
			},
		).
		Assess("verify delivery-dashboard is deployed to workload cluster",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				// Get workload cluster client via AccessRequest
				workloadConfig, err := getWorkloadClusterClient(ctx, c, tenantNamespace, "test-mcp")
				if err != nil {
					t.Errorf("failed to get workload cluster client: %v", err)
					return ctx
				}

				scheme := c.Client().Resources().GetScheme()
				workloadClient, err := client.New(workloadConfig, client.Options{
					Scheme: scheme,
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

				// Verify at least one pod exists and is Running
				podList := &corev1.PodList{}
				if err := workloadClient.List(ctx, podList, client.InNamespace("odg-system")); err != nil {
					t.Errorf("failed to list pods in odg-system: %v", err)
					return ctx
				}

				if len(podList.Items) == 0 {
					t.Errorf("no pods found in odg-system namespace")
					return ctx
				}

				runningPods := 0
				for _, pod := range podList.Items {
					if pod.Status.Phase == corev1.PodRunning {
						runningPods++
					}
				}

				if runningPods == 0 {
					t.Errorf("no running pods found in odg-system namespace (found %d pod(s) total)", len(podList.Items))
					return ctx
				}

				t.Logf("Workload deployment validated: %d running pod(s) in odg-system", runningPods)
				return ctx
			},
		).
		Assess("verify domain objects can be created", providers.ImportDomainAPIs("test-mcp", "mcp")).
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
		Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(5*time.Minute)))
	testenv.Test(t, basicProviderTest.Feature())
}
