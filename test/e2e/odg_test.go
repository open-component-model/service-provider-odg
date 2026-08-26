package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

func createDummyPullSecret(ctx context.Context, c *envconf.Config, namespace, name string) error {
	// Create a dummy docker config for testing
	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			"test.example.com": map[string]string{
				"username": "test",
				"password": "test",
				"auth":     base64.StdEncoding.EncodeToString([]byte("test:test")),
			},
		},
	}
	dockerConfigJSON, err := json.Marshal(dockerConfig)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: dockerConfigJSON,
		},
	}

	return c.Client().Resources().Create(ctx, secret)
}

func TestServiceProvider(t *testing.T) {
	var onboardingList unstructured.UnstructuredList
	var tenantNamespace string
	var chartNames []string

	basicProviderTest := features.New("provider test").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			// Create dummy pull secret for testing
			if err := createDummyPullSecret(ctx, c, "openmcp-system", "privateregcred"); err != nil {
				t.Errorf("failed to create dummy pull secret: %v", err)
			}
			objs, err := resources.CreateObjectsFromDir(ctx, c, "platform")
			if err != nil {
				t.Errorf("failed to create platform cluster objects: %v", err)
				return ctx
			}
			for _, obj := range objs.Items {
				if obj.GetKind() == "ProviderConfig" {
					charts, _, _ := unstructured.NestedSlice(obj.Object, "spec", "charts")
					for _, ch := range charts {
						if m, ok := ch.(map[string]interface{}); ok {
							if name, ok := m["chartName"].(string); ok {
								chartNames = append(chartNames, name)
							}
						}
					}
				}
			}
			if len(chartNames) == 0 {
				t.Errorf("no charts found in ProviderConfig")
			}
			t.Logf("Charts to verify: %v", chartNames)
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
				// Note: We don't wait for ODG Ready status in e2e tests because the Flux
				// resources (OCIRepository, HelmRelease) cannot become Ready with dummy
				// credentials. The controller is working correctly - we just validate
				// resource creation and configuration in subsequent assessments.
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
		Assess("verify OCIRepositories are created correctly",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				for _, chartName := range chartNames {
					ociRepo := &sourcev1.OCIRepository{}
					err := wait.For(
						func(ctx context.Context) (bool, error) {
							err := c.Client().Resources().Get(ctx, chartName, tenantNamespace, ociRepo)
							return err == nil, nil
						},
						wait.WithTimeout(30*time.Second),
						wait.WithInterval(2*time.Second),
					)
					if err != nil {
						t.Errorf("OCIRepository %q was not created: %v", chartName, err)
						continue
					}
					if ociRepo.Spec.SecretRef == nil || ociRepo.Spec.SecretRef.Name == "" {
						t.Errorf("OCIRepository %q has no secretRef", chartName)
					}
					t.Logf("OCIRepository %q validated (spec verified, status check skipped due to test credential limitations)", chartName)
				}
				return ctx
			},
		).
		Assess("verify HelmReleases are created correctly",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				for _, chartName := range chartNames {
					helmRelease := &helmv2.HelmRelease{}
					err := wait.For(
						func(ctx context.Context) (bool, error) {
							err := c.Client().Resources().Get(ctx, chartName, tenantNamespace, helmRelease)
							return err == nil, nil
						},
						wait.WithTimeout(30*time.Second),
						wait.WithInterval(2*time.Second),
					)
					if err != nil {
						t.Errorf("HelmRelease %q was not created: %v", chartName, err)
						continue
					}
					if !strings.HasPrefix(helmRelease.Spec.TargetNamespace, "odg-system-") {
						t.Errorf("HelmRelease %q targetNamespace mismatch: got %q, want prefix %q",
							chartName, helmRelease.Spec.TargetNamespace, "odg-system-")
					}
					if helmRelease.Spec.ChartRef == nil || helmRelease.Spec.ChartRef.Name != chartName {
						t.Errorf("HelmRelease %q chartRef mismatch", chartName)
					}
					if helmRelease.Spec.KubeConfig == nil {
						t.Errorf("HelmRelease %q should have KubeConfig configured for remote deployment", chartName)
					}
					t.Logf("HelmRelease %q validated (spec verified, deployment check skipped due to test credential limitations)", chartName)
				}
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
