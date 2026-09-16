package e2e

import (
	"context"
	"fmt"
	"testing"

	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
)

// getTenantNamespace calculates the tenant namespace using the same hash function as the controller.
// This ensures we look in the correct namespace for Flux resources.
func getTenantNamespace(mcpName, mcpNamespace string) (string, error) {
	return libutils.StableMCPNamespace(mcpName, mcpNamespace)
}

// getWorkloadClusterConfig returns an envconf.Config for the workload-odg cluster.
// It uses the kind provider directly (127.0.0.1) rather than the kubeconfig stored
// in the AccessRequest secret (which contains the Docker-internal IP and is unreachable
// from the test process both locally and in GitHub Actions).
func getWorkloadClusterConfig() (*envconf.Config, error) {
	cfg, err := clusterutils.ConfigByPrefix("workload-odg", "odg-system")
	if err != nil {
		return nil, fmt.Errorf("failed to get workload-odg cluster config: %w", err)
	}
	return cfg, nil
}

const (
	gatewayFinalizer       = "platformservice.openmcp.cloud/gateway"
	clusterAccessFinalizer = "clusters.openmcp.cloud/clusteraccess"
	clusterFinalizer       = "clusters.openmcp.cloud/finalizer"
)

func cleanupStuckGatewayFinalizers(ctx context.Context, t *testing.T, c *envconf.Config, namespace string) {
	clusterList := &unstructured.UnstructuredList{}
	clusterList.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "clusters.openmcp.cloud", Version: "v1alpha1", Kind: "ClusterList",
	})
	if err := c.Client().Resources().WithNamespace(namespace).List(ctx, clusterList); err != nil {
		t.Logf("failed to list clusters in %s for gateway cleanup: %v", namespace, err)
		return
	}
	for i := range clusterList.Items {
		finalizers, found, _ := unstructured.NestedStringSlice(clusterList.Items[i].Object, "metadata", "finalizers")
		if !found {
			continue
		}
		var updated []string
		for _, f := range finalizers {
			if f != gatewayFinalizer {
				updated = append(updated, f)
			}
		}
		if len(updated) == len(finalizers) {
			continue
		}
		t.Logf("removing gateway finalizer from Cluster %s/%s", namespace, clusterList.Items[i].GetName())
		_ = unstructured.SetNestedStringSlice(clusterList.Items[i].Object, updated, "metadata", "finalizers")
		if err := c.Client().Resources().Update(ctx, &clusterList.Items[i]); err != nil {
			t.Logf("failed to update Cluster %s/%s: %v", namespace, clusterList.Items[i].GetName(), err)
		}
	}

	arList := &unstructured.UnstructuredList{}
	arList.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "clusters.openmcp.cloud", Version: "v1alpha1", Kind: "AccessRequestList",
	})
	if err := c.Client().Resources().WithNamespace(namespace).List(ctx, arList); err != nil {
		t.Logf("failed to list accessrequests in %s for gateway cleanup: %v", namespace, err)
		return
	}
	for i := range arList.Items {
		finalizers, found, _ := unstructured.NestedStringSlice(arList.Items[i].Object, "metadata", "finalizers")
		if !found {
			continue
		}
		var updated []string
		for _, f := range finalizers {
			if f != clusterAccessFinalizer && f != clusterFinalizer {
				updated = append(updated, f)
			}
		}
		if len(updated) == len(finalizers) {
			continue
		}
		t.Logf("removing clusteraccess finalizer from AccessRequest %s/%s", namespace, arList.Items[i].GetName())
		_ = unstructured.SetNestedStringSlice(arList.Items[i].Object, updated, "metadata", "finalizers")
		if err := c.Client().Resources().Update(ctx, &arList.Items[i]); err != nil {
			t.Logf("failed to update AccessRequest %s/%s: %v", namespace, arList.Items[i].GetName(), err)
		}
	}
}
