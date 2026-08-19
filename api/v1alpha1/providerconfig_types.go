/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"strings"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ProviderConfigSpec defines the desired state of ProviderConfig
type ProviderConfigSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// foo is an example field of ProviderConfig. Edit providerconfig_types.go to remove/update
	// +optional
	// +kubebuilder:default:="1m"
	// +kubebuilder:validation:Format=duration
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`

	Versions []ODGVersion `json:"versions"`
}

// ODGVersion defines a version of the ODG that can be installed.
type ODGVersion struct {
	// ChartVersion is the tag of the Helm chart to install.
	// +required
	ChartVersion string `json:"chartVersion"`

	// ChartURL is a reference to an OCI repository that hosts the ODG Helm chart.
	// An "oci://" prefix is added automatically when missing.
	// +optional
	// +kubebuilder:default="oci://europe-docker.pkg.dev/gardener-project/releases/charts/odg/delivery-dashboard"
	ChartURL *string `json:"chartURL,omitempty"`

	// ChartPullSecretName is the name of a secret in the controller's namespace holding the
	// credentials to pull the Helm chart. It is replicated into the tenant namespace and
	// wired as secretRef on the OCIRepository.
	// The secret must be of type kubernetes.io/dockerconfigjson.
	// +optional
	ChartPullSecretName string `json:"chartPullSecretName,omitempty"`

	// HelmValues are arbitrary Helm values passed directly to the managed HelmRelease.
	// Secrets referenced under `manager.imagePullSecrets` are replicated from the
	// controller's namespace into the ODG namespace on the control plane.
	// +optional
	HelmValues *apiextensionsv1.JSON `json:"helmValues,omitempty"`
}

// ProviderConfigStatus defines the observed state of ProviderConfig.
type ProviderConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the ProviderConfig resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ProviderConfig is the Schema for the providerconfigs API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
type ProviderConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ProviderConfig
	// +required
	Spec ProviderConfigSpec `json:"spec"`

	// status defines the observed state of ProviderConfig
	// +optional
	Status ProviderConfigStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ProviderConfigList contains a list of ProviderConfig
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ProviderConfig{}, &ProviderConfigList{})
		return nil
	})
}

// PollInterval returns the poll interval duration from the spec.
func (o *ProviderConfig) PollInterval() time.Duration {
	// TODO pollInterval has to be required
	return o.Spec.PollInterval.Duration
}

// EnsureOCIScheme prefixes the given URL with "oci://" unless it already has the scheme.
func EnsureOCIScheme(url string) string {
	if !strings.HasPrefix(url, "oci://") {
		return "oci://" + url
	}
	return url
}
