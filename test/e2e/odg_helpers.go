package e2e

import (
	"context"
	"fmt"
	"testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// getTenantNamespace calculates the tenant namespace using the same hash function as the controller.
// This ensures we look in the correct namespace for Flux resources.
func getTenantNamespace(mcpName, mcpNamespace string) (string, error) {
	return libutils.StableMCPNamespace(mcpName, mcpNamespace)
}

// getWorkloadClusterClient retrieves the workload cluster kubeconfig from the AccessRequest
// and creates a REST config for accessing the workload cluster.
// The AccessRequest is created by the advanced cluster access reconciler and contains
// the kubeconfig secret reference in its status.
func getWorkloadClusterClient(ctx context.Context, platformCfg *envconf.Config, tenantNamespace, mcpName string) (*rest.Config, error) {
	// AccessRequest naming follows the pattern used by the advanced cluster access reconciler:
	// providerName + "--" + objectName + requestSuffixWorkload
	// where providerName = "odg" and requestSuffixWorkload = "--wl-odg"
	accessRequestName := fmt.Sprintf("odg--%s--wl-odg", mcpName)

	accessRequest := &clustersv1alpha1.AccessRequest{}
	err := platformCfg.Client().Resources().Get(ctx, accessRequestName, tenantNamespace, accessRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get AccessRequest %s/%s: %w", tenantNamespace, accessRequestName, err)
	}

	if accessRequest.Status.SecretRef == nil {
		return nil, fmt.Errorf("AccessRequest %s/%s has no SecretRef in status", tenantNamespace, accessRequestName)
	}

	// Get the secret containing the kubeconfig
	secret := &corev1.Secret{}
	err = platformCfg.Client().Resources().Get(ctx, accessRequest.Status.SecretRef.Name, tenantNamespace, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig secret %s/%s: %w", tenantNamespace, accessRequest.Status.SecretRef.Name, err)
	}

	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s does not contain kubeconfig key", tenantNamespace, accessRequest.Status.SecretRef.Name)
	}

	// Parse kubeconfig and create REST config
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	return restConfig, nil
}

const (
	gatewayFinalizer       = "platformservice.openmcp.cloud/gateway"
	clusterAccessFinalizer = "clusters.openmcp.cloud/clusteraccess"
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
			if f != clusterAccessFinalizer {
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
