package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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
				objList.DeepCopyInto(&onboardingList)

				tenantNamespace, err = getTenantNamespace("test-mcp", objList.Items[0].GetNamespace())
				if err != nil {
					t.Errorf("failed to calculate tenant namespace: %v", err)
					return ctx
				}
				t.Logf("Calculated tenant namespace: %s", tenantNamespace)

				return ctx
			},
		).
		Assess("verify OCIRepositories are created and ready",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				for _, chartName := range chartNames {
					ociRepo := &sourcev1.OCIRepository{}
					err := wait.For(
						func(ctx context.Context) (bool, error) {
							err := c.Client().Resources().Get(ctx, chartName, tenantNamespace, ociRepo)
							if err != nil {
								return false, nil
							}
							return apimeta.IsStatusConditionTrue(ociRepo.Status.Conditions, "Ready"), nil
						},
						wait.WithTimeout(3*time.Minute),
						wait.WithInterval(5*time.Second),
					)
					if err != nil {
						t.Errorf("OCIRepository %q did not become Ready: %v", chartName, err)
						continue
					}
					if ociRepo.Spec.SecretRef == nil || ociRepo.Spec.SecretRef.Name == "" {
						t.Errorf("OCIRepository %q has no secretRef", chartName)
					} else {
						t.Logf("OCIRepository %q has secretRef: %s", chartName, ociRepo.Spec.SecretRef.Name)
					}
					t.Logf("OCIRepository %q is Ready", chartName)
				}
				return ctx
			},
		).
		Assess("verify HelmReleases are created correctly",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				// notReadyCharts lists charts that are not expected to become Ready in the test
				// environment. Newly added charts are expected to become Ready by default.
				notReadyCharts := map[string]bool{
					"delivery-service": true,
				}
				for _, chartName := range chartNames {
					helmRelease := &helmv2.HelmRelease{}
					err := wait.For(
						func(ctx context.Context) (bool, error) {
							err := c.Client().Resources().Get(ctx, chartName, tenantNamespace, helmRelease)
							if err != nil {
								return false, nil
							}
							if helmRelease.Spec.TargetNamespace != "odg-system" {
								return false, nil
							}
							if helmRelease.Spec.ChartRef == nil || helmRelease.Spec.ChartRef.Name != chartName {
								return false, nil
							}
							if helmRelease.Spec.KubeConfig == nil {
								return false, nil
							}
							if notReadyCharts[chartName] {
								return true, nil
							}
							return apimeta.IsStatusConditionTrue(helmRelease.Status.Conditions, "Ready"), nil
						},
						wait.WithTimeout(5*time.Minute),
						wait.WithInterval(5*time.Second),
					)
					if err != nil {
						t.Errorf("HelmRelease %q was not created or did not meet spec: %v", chartName, err)
						continue
					}
					if notReadyCharts[chartName] {
						t.Logf("HelmRelease %q validated (spec verified, Ready check skipped - requires runtime config)", chartName)
					} else {
						t.Logf("HelmRelease %q is Ready", chartName)
					}
				}
				return ctx
			},
		).
		Assess("verify bootstrapping values secret merges ConfigMap and Secret refs",
			func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
				valuesSecret := &corev1.Secret{}
				err := wait.For(
					func(ctx context.Context) (bool, error) {
						err := c.Client().Resources().Get(ctx, "bootstrapping-values", tenantNamespace, valuesSecret)
						return err == nil, nil
					},
					wait.WithTimeout(30*time.Second),
					wait.WithInterval(2*time.Second),
				)
				if err != nil {
					t.Errorf("bootstrapping-values Secret was not created: %v", err)
					return ctx
				}

				raw, ok := valuesSecret.Data["values.yaml"]
				if !ok {
					t.Errorf("bootstrapping-values Secret has no values.yaml key")
					return ctx
				}

				var merged map[string]any
				if err := json.Unmarshal(raw, &merged); err != nil {
					t.Errorf("bootstrapping-values Secret values.yaml is not valid JSON: %v", err)
					return ctx
				}

				// ConfigMap contribution: extensions_cfg key must be present
				if _, ok := merged["extensions_cfg"]; !ok {
					t.Errorf("bootstrapping-values missing extensions_cfg (expected from ConfigurationRef ConfigMap)")
				}

				// Secret contribution: secrets key must be present and override/extend ConfigMap
				if _, ok := merged["secrets"]; !ok {
					t.Errorf("bootstrapping-values missing secrets (expected from SecretsRef Secret)")
				}

				t.Logf("bootstrapping-values Secret contains merged keys: %v", func() []string {
					keys := make([]string, 0, len(merged))
					for k := range merged {
						keys = append(keys, k)
					}
					return keys
				}())
				return ctx
			},
		).
		Assess("verify domain objects can be created", providers.ImportDomainAPIs("test-mcp", "mcp")).
		Teardown(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if *keepClusters {
				t.Logf("--keep-clusters set: skipping onboarding teardown")
				return ctx
			}
			onboardingConfig, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Error(err)
				return ctx
			}
			for _, obj := range onboardingList.Items {
				if err := resources.DeleteObject(ctx, onboardingConfig, &obj, wait.WithTimeout(8*time.Minute)); err != nil {
					t.Errorf("failed to delete onboarding object: %v", err)
				}
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if *keepClusters {
				t.Logf("--keep-clusters set: skipping MCP teardown")
				return ctx
			}
			cleanupStuckGatewayFinalizers(ctx, t, c, tenantNamespace)
			return providers.DeleteMCP("test-mcp", wait.WithTimeout(8*time.Minute))(ctx, t, c)
		})
	testenv.Test(t, basicProviderTest.Feature())
}
