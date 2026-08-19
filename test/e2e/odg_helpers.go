package e2e

import (
	"context"
	"fmt"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
	corev1 "k8s.io/api/core/v1"
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
	// AccessRequest naming follows the pattern used in the controller:
	// stableRequestNameFromLocalName(providerName, objectName) + requestSuffixWorkload
	// where providerName = "odg" and requestSuffixWorkload = "--wl"
	// The stable naming includes the full group name
	accessRequestName := fmt.Sprintf("odg.services.open-control-plane.io--%s--wl", mcpName)

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
