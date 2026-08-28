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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	clusteraccess "github.com/openmcp-project/opencontrolplane-runtime/pkg/serviceprovider/clusteraccess"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess/advanced"

	"github.com/openmcp-project/opencontrolplane-runtime/pkg/serviceprovider"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	apiv1alpha1 "github.com/open-component-model/service-provider-odg/api/v1alpha1"
)

// ODGReconciler reconciles a ODG object
type ODGReconciler struct {
	// OnboardingCluster is the cluster where this controller watches ODG resources and reacts to their changes.
	OnboardingCluster *clusters.Cluster
	// PlatformCluster is the cluster where this controller is deployed and configured.
	PlatformCluster *clusters.Cluster
	// PodNamespace is the namespace where this controller is deployed in.
	PodNamespace string
	// ProviderName is the name of the service provider.
	ProviderName string
}

const (
	// conditionReasonError is the Ready condition reason used when a reconcile step fails.
	conditionReasonError = "ReconcileError"

	// OdgSystemNamespacePrefix is the namespace prefix on the target cluster to deploy ODG components into.
	OdgSystemNamespacePrefix = "odg-system"

	// requestSuffixWorkload is the suffix used for the access request of the workload cluster.
	requestSuffixWorkload = "--wl"

	// helmValuesSuffix is the suffix used for the name of the secrets containing the Helm values
	helmValuesSuffix = "-values"

	// ociRepositoryKind and helmReleaseKind are used for status resource entries.
	ociRepositoryKind = "OCIRepository"
	helmReleaseKind   = "HelmRelease"

	// managedByLabel is set on all objects created by this controller to distinguish them from
	// objects created by other service providers in the same tenant namespace.
	managedByLabel      = "app.kubernetes.io/managed-by"
	managedByLabelValue = "service-provider-odg"
)

// CreateOrUpdate is called on every add or update event
func (r *ODGReconciler) CreateOrUpdate(ctx context.Context, svcobj *apiv1alpha1.ODG, providerConfig *apiv1alpha1.ProviderConfig, clusters clusteraccess.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	serviceprovider.StatusProgressing(svcobj, "Reconciling", "Reconcile in progress")

	tenantNamespace, err := libutils.StableMCPNamespace(svcobj.Name, svcobj.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to determine stable namespace for ODG instance: %w", err)
	}

	if err := r.ensureTenantNamespace(ctx, tenantNamespace); err != nil {
		serviceprovider.StatusProgressing(svcobj, conditionReasonError, err.Error())
		return ctrl.Result{}, err
	}

	allReady := true
	var resources []apiv1alpha1.ManagedResource

	for _, chart := range providerConfig.Spec.Charts {
		chartResources, err := r.reconcileChart(ctx, svcobj, chart, tenantNamespace, clusters)
		if err != nil {
			serviceprovider.StatusProgressing(svcobj, conditionReasonError, err.Error())
			return ctrl.Result{}, err
		}
		for _, res := range chartResources {
			if res.Phase != apiv1alpha1.Ready {
				allReady = false
			}
		}
		resources = append(resources, chartResources...)
	}

	if err := r.deleteRemovedCharts(ctx, tenantNamespace, providerConfig.Spec.Charts); err != nil {
		serviceprovider.StatusProgressing(svcobj, conditionReasonError, err.Error())
		return ctrl.Result{}, err
	}

	l.Info("Done reconciling ODG resource", "odgName", svcobj.Name)

	svcobj.Status.Resources = resources

	if allReady {
		serviceprovider.StatusReady(svcobj)
	} else {
		serviceprovider.StatusProgressing(svcobj, "Reconciling", "Waiting for managed resources to become ready")
	}
	return ctrl.Result{}, nil
}

func (r *ODGReconciler) reconcileChart(ctx context.Context, svcobj *apiv1alpha1.ODG, chart apiv1alpha1.ODGChart, tenantNamespace string, clusters clusteraccess.ClusterContext) ([]apiv1alpha1.ManagedResource, error) {
	odgNamespace := StableODGNamespace(svcobj.Namespace, svcobj.Name)

	if err := r.replicateChartPullSecret(ctx, chart.ChartPullSecretName, types.NamespacedName{Name: chart.ChartPullSecretName, Namespace: tenantNamespace}); err != nil {
		return nil, fmt.Errorf("failed to replicate chart pull secret: %w", err)
	}

	ociRepo, err := r.createOrUpdateOCIRepository(ctx, chart.ChartName, apiv1alpha1.EnsureOCIScheme(*chart.ChartURL), chart.ChartVersion, chart.ChartPullSecretName, tenantNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile OCI Repository: %w", err)
	}

	if err := r.replicateWorkloadImagePullSecrets(ctx, clusters.WorkloadCluster, chart.ChartPullSecretName, odgNamespace); err != nil {
		return nil, fmt.Errorf("failed to replicate workload cluster image pull secrets: %w", err)
	}

	helmValues := chart.HelmValues
	if chart.ChartName == "bootstrapping" {
		var err error
		helmValues, err = r.mergeODGConfiguration(ctx, helmValues, svcobj)
		if err != nil {
			return nil, fmt.Errorf("failed to merge ODG configuration into helm values: %w", err)
		}
	}

	valuesSecretName := chart.ChartName + helmValuesSuffix
	if err := r.createOrUpdateValuesSecret(ctx, valuesSecretName, tenantNamespace, helmValues); err != nil {
		return nil, fmt.Errorf("failed to write helm values secret: %w", err)
	}

	helmRel, err := r.createOrUpdateHelmRelease(ctx, chart.ChartName, tenantNamespace, odgNamespace, valuesSecretName, svcobj)
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile HelmRelease: %w", err)
	}

	ociPhase, ociMsg := resourceStatus(ociRepo.Status.Conditions)
	hrPhase, hrMsg := resourceStatus(helmRel.Status.Conditions)

	return []apiv1alpha1.ManagedResource{
		{
			TypedObjectReference: corev1.TypedObjectReference{
				APIGroup:  stringPtr(sourcev1.GroupVersion.Group),
				Kind:      ociRepositoryKind,
				Name:      chart.ChartName,
				Namespace: &tenantNamespace,
			},
			Phase:    ociPhase,
			Message:  ociMsg,
			Location: apiv1alpha1.PlatformCluster,
		},
		{
			TypedObjectReference: corev1.TypedObjectReference{
				Kind:      "Secret",
				Name:      valuesSecretName,
				Namespace: &tenantNamespace,
			},
			Phase:    apiv1alpha1.Ready,
			Location: apiv1alpha1.PlatformCluster,
		},
		{
			TypedObjectReference: corev1.TypedObjectReference{
				APIGroup:  stringPtr(helmv2.GroupVersion.Group),
				Kind:      helmReleaseKind,
				Name:      chart.ChartName,
				Namespace: &tenantNamespace,
			},
			Phase:    hrPhase,
			Message:  hrMsg,
			Location: apiv1alpha1.PlatformCluster,
		},
	}, nil
}

// Delete is called on every delete event
func (r *ODGReconciler) Delete(ctx context.Context, obj *apiv1alpha1.ODG, providerConfig *apiv1alpha1.ProviderConfig, clusters clusteraccess.ClusterContext) (ctrl.Result, error) {
	serviceprovider.StatusTerminating(obj)

	tenantNamespace, err := libutils.StableMCPNamespace(obj.Name, obj.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to determine stable namespace for ODG instance: %w", err)
	}
	obj.Status.Resources = managedResources(tenantNamespace, providerConfig.Spec.Charts, apiv1alpha1.Terminating)

	var objects []client.Object
	for _, chart := range providerConfig.Spec.Charts {
		objects = append(objects,
			&sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: chart.ChartName, Namespace: tenantNamespace},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: chart.ChartName + helmValuesSuffix, Namespace: tenantNamespace},
			},
			&helmv2.HelmRelease{
				ObjectMeta: metav1.ObjectMeta{Name: chart.ChartName, Namespace: tenantNamespace},
			},
		)
	}

	objectsStillExist := false
	for _, managedObj := range objects {
		if err := r.PlatformCluster.Client().Delete(ctx, managedObj); client.IgnoreNotFound(err) != nil {
			serviceprovider.StatusTerminatingWithReason(obj, conditionReasonError, err.Error())
			return ctrl.Result{}, fmt.Errorf("delete object failed: %w", err)
		}
		if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(managedObj), managedObj); !apierrors.IsNotFound(err) {
			objectsStillExist = true
		}
	}

	if clusters.WorkloadCluster != nil {
		odgNamespace := StableODGNamespace(obj.Namespace, obj.Name)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: odgNamespace}}
		if err := clusters.WorkloadCluster.Client().Delete(ctx, ns); client.IgnoreNotFound(err) != nil {
			serviceprovider.StatusTerminatingWithReason(obj, conditionReasonError, err.Error())
			return ctrl.Result{}, fmt.Errorf("failed to delete odg namespace %q: %w", odgNamespace, err)
		}
		if err := clusters.WorkloadCluster.Client().Get(ctx, client.ObjectKey{Name: odgNamespace}, ns); !apierrors.IsNotFound(err) {
			objectsStillExist = true
		}
	}

	if objectsStillExist {
		return ctrl.Result{
			RequeueAfter: time.Second * 10,
		}, nil
	}

	obj.Status.Resources = nil
	serviceprovider.StatusReady(obj)
	return ctrl.Result{}, nil
}

// IsReferencedSecret returns true if the given secret should trigger
// reconciliation. See serviceprovider.SecretWatcher for details.
//
//revive:disable:unused-parameter
func (r *ODGReconciler) IsReferencedSecret(ctx context.Context, secret *corev1.Secret, pc *apiv1alpha1.ProviderConfig) bool {
	if pc == nil {
		return false
	}
	// TODO: Check if the secret is referenced in the provider config, for example:
	// for _, ref := range pc.Spec.ImagePullSecrets {
	//     if ref.Name == secret.Name {
	//         return true
	//     }
	// }
	return false
}

func createOciRepository(name, chartURL, secretName, chartVersion, namespace string) *sourcev1.OCIRepository {
	var secretRef *meta.LocalObjectReference
	if secretName != "" {
		secretRef = &meta.LocalObjectReference{Name: secretName}
	}

	return &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{managedByLabel: managedByLabelValue},
		},
		Spec: sourcev1.OCIRepositorySpec{
			Interval:  metav1.Duration{Duration: time.Minute},
			URL:       chartURL,
			SecretRef: secretRef,
			Reference: &sourcev1.OCIRepositoryRef{
				Tag: chartVersion,
			},
		},
	}
}

func (r *ODGReconciler) createOrUpdateOCIRepository(ctx context.Context, name, chartURL, chartVersion, secretName, namespace string) (*sourcev1.OCIRepository, error) {
	ociRepository := createOciRepository(name, chartURL, secretName, chartVersion, namespace)
	managedObj := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ociRepository.Name,
			Namespace: namespace,
		},
	}
	l := logf.FromContext(ctx)
	l.Info("creating OCI Repository", "object", ociRepository)
	if _, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), managedObj, func() error {
		managedObj.Labels = map[string]string{managedByLabel: managedByLabelValue}
		managedObj.Spec = ociRepository.Spec
		return nil
	}); err != nil {
		return nil, err
	}

	return managedObj, nil
}

// replicateChartPullSecret copies the named secret from the controller's namespace into the
// tenant namespace on the platform cluster, where the OCIRepository references it.
func (r *ODGReconciler) replicateChartPullSecret(ctx context.Context, secretName string, target types.NamespacedName) error {
	if secretName == "" {
		return nil
	}
	platformClient := r.PlatformCluster.Client()

	sourceSecret := &corev1.Secret{}
	sourceKey := client.ObjectKey{Name: secretName, Namespace: r.PodNamespace}
	if err := platformClient.Get(ctx, sourceKey, sourceSecret); err != nil {
		return fmt.Errorf("failed to get chart pull secret %q from namespace %q: %w", secretName, r.PodNamespace, err)
	}

	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      target.Name,
			Namespace: target.Namespace,
		},
	}
	if _, err := ctrl.CreateOrUpdate(ctx, platformClient, targetSecret, func() error {
		targetSecret.Data = sourceSecret.Data
		targetSecret.Type = sourceSecret.Type
		return nil
	}); err != nil {
		return fmt.Errorf("failed to replicate chart pull secret %q to namespace %q: %w", secretName, target.Namespace, err)
	}

	return nil
}

// ensureTenantNamespace ensures the tenant namespace exists on the platform cluster.
func (r *ODGReconciler) ensureTenantNamespace(ctx context.Context, tenantNamespace string) error {
	l := logf.FromContext(ctx)
	tenantNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenantNamespace,
		},
	}
	if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKey{Name: tenantNamespace}, tenantNs); err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.PlatformCluster.Client().Create(ctx, tenantNs); err != nil {
				return fmt.Errorf("failed to create tenant namespace %q: %w", tenantNamespace, err)
			}
			l.Info("Created tenant namespace on platform cluster", "namespace", tenantNamespace)
		} else {
			return fmt.Errorf("failed to check tenant namespace %q: %w", tenantNamespace, err)
		}
	}
	return nil
}

func (r *ODGReconciler) replicateWorkloadImagePullSecrets(ctx context.Context, workloadCluster *clusters.Cluster, secretName, odgNamespace string) error {
	if secretName == "" {
		return nil
	}

	if workloadCluster == nil {
		return nil
	}

	platformClient := r.PlatformCluster.Client()
	workloadClient := workloadCluster.Client()

	sourceSecret := &corev1.Secret{}
	sourceKey := client.ObjectKey{Name: secretName, Namespace: r.PodNamespace}

	if err := platformClient.Get(ctx, sourceKey, sourceSecret); err != nil {
		return fmt.Errorf("failed to get chart pull secret %q from namespace %q: %w", secretName, r.PodNamespace, err)
	}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: odgNamespace}}
	if err := workloadClient.Get(ctx, client.ObjectKey{Name: odgNamespace}, ns); apierrors.IsNotFound(err) {
		if err := workloadClient.Create(ctx, ns); err != nil {
			return fmt.Errorf("failed to create namespace %q on workload cluster: %w", odgNamespace, err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to check namespace %q on workload cluster: %w", odgNamespace, err)
	}

	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: odgNamespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(ctx, workloadClient, targetSecret, func() error {
		targetSecret.Data = sourceSecret.Data
		targetSecret.Type = sourceSecret.Type
		return nil
	}); err != nil {
		return fmt.Errorf("failed to replicate chart pull secret %q to namespace %q: %w", secretName, odgNamespace, err)
	}

	return nil
}

func (r *ODGReconciler) createOrUpdateValuesSecret(ctx context.Context, name, namespace string, values *apiextensionsv1.JSON) error {
	var raw []byte
	if values != nil {
		raw = values.Raw
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	_, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), secret, func() error {
		secret.Labels = map[string]string{managedByLabel: managedByLabelValue}
		secret.Data = map[string][]byte{"values.yaml": raw}
		return nil
	})
	return err
}

func (r *ODGReconciler) createOrUpdateHelmRelease(ctx context.Context, name, tenantNamespace, odgNamespace, valuesSecretName string, svcobj *apiv1alpha1.ODG) (*helmv2.HelmRelease, error) {
	helmRelease, err := r.createHelmRelease(ctx, name, tenantNamespace, odgNamespace, valuesSecretName, svcobj)
	if err != nil {
		return nil, fmt.Errorf("failed to create helm release: %w", err)
	}
	managedObj := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmRelease.Name,
			Namespace: tenantNamespace,
		},
	}
	l := logf.FromContext(ctx)
	l.Info("creating Helm Release", "object", managedObj)
	if _, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), managedObj, func() error {
		managedObj.Labels = map[string]string{managedByLabel: managedByLabelValue}
		managedObj.Spec = helmRelease.Spec
		return nil
	}); err != nil {
		return nil, err
	}

	return managedObj, nil
}

func (r *ODGReconciler) createHelmRelease(ctx context.Context, name, tenantNamespace, odgNamespace, valuesSecretName string, svcobj *apiv1alpha1.ODG) (*helmv2.HelmRelease, error) {
	fluxConfigRef, err := r.getWorkloadFluxConfig(ctx, tenantNamespace, svcobj.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get FluxConfig: %w", err)
	}

	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tenantNamespace,
			Labels:    map[string]string{managedByLabel: managedByLabelValue},
		},
		Spec: helmv2.HelmReleaseSpec{
			ReleaseName:      name,
			Interval:         metav1.Duration{Duration: time.Minute},
			TargetNamespace:  odgNamespace,
			StorageNamespace: odgNamespace,
			Install: &helmv2.Install{
				CRDs:            helmv2.Create,
				CreateNamespace: true,
				Remediation: &helmv2.InstallRemediation{
					Retries: 3,
				},
			},
			Upgrade: &helmv2.Upgrade{
				CRDs:          helmv2.CreateReplace,
				CleanupOnFail: true,
				Remediation: &helmv2.UpgradeRemediation{
					Retries:  3,
					Strategy: remediationStrategyPointer(helmv2.RollbackRemediationStrategy),
				},
			},
			ChartRef: &helmv2.CrossNamespaceSourceReference{
				Kind:      ociRepositoryKind,
				Name:      name,
				Namespace: tenantNamespace,
			},
			ValuesFrom: []helmv2.ValuesReference{{
				Kind: "Secret",
				Name: valuesSecretName,
			}},
			KubeConfig: &meta.KubeConfigReference{
				SecretRef: fluxConfigRef,
			},
		},
	}, nil
}

// stableRequestNameFromLocalName works like StableRequestName but takes a local name directly instead of a reconcile.Request.
func stableRequestNameFromLocalName(controllerName, localName string) string {
	return advanced.StableRequestNameFromLocalName(controllerName, localName, "")
}

func (r *ODGReconciler) getWorkloadFluxConfig(ctx context.Context, namespace, objectName string) (*meta.SecretKeyReference, error) {
	workloadAccessRequest := &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stableRequestNameFromLocalName(r.ProviderName, objectName) + requestSuffixWorkload,
			Namespace: namespace,
		},
	}

	if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(workloadAccessRequest), workloadAccessRequest); err != nil {
		return nil, fmt.Errorf("failed to get Workload AccessRequest: %w", err)
	}

	return &meta.SecretKeyReference{
		Name: workloadAccessRequest.Status.SecretRef.Name,
		Key:  "kubeconfig",
	}, nil
}

// resourceStatus maps a Flux resource's Ready condition to an InstancePhase.
// Returns Ready with an empty message when ready, otherwise Progressing with
// the Ready condition's message (or empty if the condition is absent).
func resourceStatus(conditions []metav1.Condition) (apiv1alpha1.InstancePhase, string) {
	if apimeta.IsStatusConditionTrue(conditions, meta.ReadyCondition) {
		return apiv1alpha1.Ready, ""
	}
	if cond := apimeta.FindStatusCondition(conditions, meta.ReadyCondition); cond != nil {
		return apiv1alpha1.Progressing, cond.Message
	}
	return apiv1alpha1.Progressing, ""
}

func stringPtr(s string) *string {
	return &s
}

func remediationStrategyPointer(s helmv2.RemediationStrategy) *helmv2.RemediationStrategy {
	return &s
}

// managedResources returns the set of platform-cluster objects this controller
// owns for an ODG instance, tagged with the given lifecycle phase.
func managedResources(tenantNamespace string, charts []apiv1alpha1.ODGChart, phase apiv1alpha1.InstancePhase) []apiv1alpha1.ManagedResource {
	resources := make([]apiv1alpha1.ManagedResource, 0, len(charts)*2)
	for _, chart := range charts {
		resources = append(resources,
			apiv1alpha1.ManagedResource{
				TypedObjectReference: corev1.TypedObjectReference{
					APIGroup:  stringPtr(sourcev1.GroupVersion.Group),
					Kind:      ociRepositoryKind,
					Name:      chart.ChartName,
					Namespace: stringPtr(tenantNamespace),
				},
				Phase:    phase,
				Location: apiv1alpha1.PlatformCluster,
			},
			apiv1alpha1.ManagedResource{
				TypedObjectReference: corev1.TypedObjectReference{
					Kind:      "Secret",
					Name:      chart.ChartName + helmValuesSuffix,
					Namespace: stringPtr(tenantNamespace),
				},
				Phase:    phase,
				Location: apiv1alpha1.PlatformCluster,
			},
			apiv1alpha1.ManagedResource{
				TypedObjectReference: corev1.TypedObjectReference{
					APIGroup:  stringPtr(helmv2.GroupVersion.Group),
					Kind:      helmReleaseKind,
					Name:      chart.ChartName,
					Namespace: stringPtr(tenantNamespace),
				},
				Phase:    phase,
				Location: apiv1alpha1.PlatformCluster,
			},
		)
	}
	return resources
}

// deleteRemovedCharts deletes OCIRepository and HelmRelease objects in tenantNamespace
// that are no longer referenced by any chart in the current provider config.
func (r *ODGReconciler) deleteRemovedCharts(ctx context.Context, tenantNamespace string, charts []apiv1alpha1.ODGChart) error {
	desired := make(map[string]bool, len(charts))
	for _, ch := range charts {
		desired[ch.ChartName] = true
	}

	ociList := &sourcev1.OCIRepositoryList{}
	if err := r.PlatformCluster.Client().List(ctx, ociList,
		client.InNamespace(tenantNamespace),
		client.MatchingLabels{managedByLabel: managedByLabelValue},
	); err != nil {
		return fmt.Errorf("failed to list OCIRepositories: %w", err)
	}
	for i := range ociList.Items {
		if !desired[ociList.Items[i].Name] {
			if err := r.PlatformCluster.Client().Delete(ctx, &ociList.Items[i]); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete OCIRepository %q: %w", ociList.Items[i].Name, err)
			}
		}
	}

	hrList := &helmv2.HelmReleaseList{}
	if err := r.PlatformCluster.Client().List(ctx, hrList,
		client.InNamespace(tenantNamespace),
		client.MatchingLabels{managedByLabel: managedByLabelValue},
	); err != nil {
		return fmt.Errorf("failed to list HelmReleases: %w", err)
	}
	for i := range hrList.Items {
		if !desired[hrList.Items[i].Name] {
			if err := r.deleteRemovedChart(ctx, tenantNamespace, hrList.Items[i].Name); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ODGReconciler) deleteRemovedChart(ctx context.Context, tenantNamespace, chartName string) error {
	hr := &helmv2.HelmRelease{ObjectMeta: metav1.ObjectMeta{Name: chartName, Namespace: tenantNamespace}}
	if err := r.PlatformCluster.Client().Delete(ctx, hr); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete HelmRelease %q: %w", chartName, err)
	}
	secretName := chartName + helmValuesSuffix
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: tenantNamespace}}
	if err := r.PlatformCluster.Client().Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete values secret %q: %w", secretName, err)
	}
	return nil
}

// StableODGNamespace computes the namespace on the workload cluster that belongs to the given ODG.
// onboardingName and onboardingNamespace are name and namespace of the ODG resource on the onboarding cluster.
func StableODGNamespace(onboardingNamespace, onboardingName string) string {
	// Use a tenant agnostic namespace for now (assume each ODG runs in a dedicated cluster)
	// res := controller.NameHashSHAKE128Base32(onboardingNamespace, onboardingName)
	return OdgSystemNamespacePrefix
}

// mergeODGConfiguration fetches the ConfigMap and Secret referenced in the ODG spec,
// parses the "values.yaml" key from each as JSON, and merges them (Secret on top) into base.
func (r *ODGReconciler) mergeODGConfiguration(ctx context.Context, base *apiextensionsv1.JSON, svcobj *apiv1alpha1.ODG) (*apiextensionsv1.JSON, error) {
	result := base

	if ref := svcobj.Spec.ConfigurationRef; ref != nil {
		cm := &corev1.ConfigMap{}
		if err := r.OnboardingCluster.Client().Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: svcobj.Namespace}, cm); err != nil {
			return nil, fmt.Errorf("failed to get ConfigMap %q: %w", ref.Name, err)
		}
		overlay, err := dataToJSON([]byte(cm.Data["values.yaml"]))
		if err != nil {
			return nil, fmt.Errorf("ConfigMap %q values.yaml: %w", ref.Name, err)
		}
		if result, err = mergeHelmValues(result, overlay); err != nil {
			return nil, err
		}
	}

	if ref := svcobj.Spec.SecretsRef; ref != nil {
		secret := &corev1.Secret{}
		if err := r.OnboardingCluster.Client().Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: svcobj.Namespace}, secret); err != nil {
			return nil, fmt.Errorf("failed to get Secret %q: %w", ref.Name, err)
		}
		overlay, err := dataToJSON(secret.Data["values.yaml"])
		if err != nil {
			return nil, fmt.Errorf("secret %q values.yaml: %w", ref.Name, err)
		}
		if result, err = mergeHelmValues(result, overlay); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// dataToJSON converts raw YAML or JSON bytes to canonical JSON for merging.
// Returns nil (not an error) when data is empty.
func dataToJSON(data []byte) (*apiextensionsv1.JSON, error) {
	if len(data) == 0 {
		return nil, nil
	}
	jsonBytes, err := sigsyaml.YAMLToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("yaml to json: %w", err)
	}
	return &apiextensionsv1.JSON{Raw: jsonBytes}, nil
}

// mergeHelmValues performs a deep JSON merge of overlay on top of base.
// Map values are merged recursively; all other types are overwritten by the overlay.
// Returns nil when both inputs are nil.
func mergeHelmValues(base, overlay *apiextensionsv1.JSON) (*apiextensionsv1.JSON, error) {
	if overlay == nil {
		return base, nil
	}
	if base == nil {
		return overlay, nil
	}

	var baseMap, overlayMap map[string]any
	if err := json.Unmarshal(base.Raw, &baseMap); err != nil {
		return nil, fmt.Errorf("unmarshal base helm values: %w", err)
	}
	if err := json.Unmarshal(overlay.Raw, &overlayMap); err != nil {
		return nil, fmt.Errorf("unmarshal overlay helm values: %w", err)
	}

	deepMerge(baseMap, overlayMap)

	raw, err := json.Marshal(baseMap)
	if err != nil {
		return nil, fmt.Errorf("marshal merged helm values: %w", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}, nil
}

// deepMerge merges src into dst in place. Nested maps are merged recursively;
// any other type in src overwrites the value in dst.
func deepMerge(dst, src map[string]any) {
	for k, srcVal := range src {
		dstVal, exists := dst[k]
		if !exists {
			dst[k] = srcVal
			continue
		}
		srcMap, srcIsMap := srcVal.(map[string]any)
		dstMap, dstIsMap := dstVal.(map[string]any)
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
		} else {
			dst[k] = srcVal
		}
	}
}
