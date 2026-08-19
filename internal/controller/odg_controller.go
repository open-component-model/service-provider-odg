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

	// OCIRepositoryName todo: one repo name per chart
	OCIRepositoryName = "odg-dashboard"

	// OdgSystemNamespace todo: might require multi-tenancy
	OdgSystemNamespace = "odg-system"

	// HelmReleaseName is the release name of the helm installation
	HelmReleaseName = "odg"

	// requestSuffixWorkload is the suffix used for the workload cluster.
	requestSuffixWorkload = "--wl"
)

// CreateOrUpdate is called on every add or update event
func (r *ODGReconciler) CreateOrUpdate(ctx context.Context, svcobj *apiv1alpha1.ODG, providerConfig *apiv1alpha1.ProviderConfig, clusters clusteraccess.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	serviceprovider.StatusProgressing(svcobj, "Reconciling", "Reconcile in progress")

	version := providerConfig.Spec.Versions[0]
	tenantNamespace, err := libutils.StableMCPNamespace(svcobj.Name, svcobj.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to determine stable namespace for ODG instance: %w", err)
	}

	if err := r.ensureTenantNamespace(ctx, tenantNamespace); err != nil {
		serviceprovider.StatusProgressing(svcobj, conditionReasonError, err.Error())
		return ctrl.Result{}, err
	}

	if err := r.replicateChartPullSecret(ctx, version.ChartPullSecretName, types.NamespacedName{Name: version.ChartPullSecretName, Namespace: tenantNamespace}); err != nil {
		serviceprovider.StatusProgressing(svcobj, conditionReasonError, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to replicate chart pull secret: %w", err)
	}

	chartURL := apiv1alpha1.EnsureOCIScheme(*version.ChartURL)

	ociRepo, err := r.createOrUpdateOCIRepository(ctx, chartURL, version.ChartVersion, version.ChartPullSecretName, tenantNamespace)
	if err != nil {
		serviceprovider.StatusProgressing(svcobj, conditionReasonError, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to reconcile OCI Repository: %w", err)
	}

	if err := r.replicateWorkloadImagePullSecrets(ctx, clusters.WorkloadCluster, version.ChartPullSecretName); err != nil {
		serviceprovider.StatusProgressing(svcobj, conditionReasonError, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to replicate workload cluster image pull secrets: %w", err)
	}

	helmRel, err := r.createOrUpdateHelmRelease(ctx, tenantNamespace, svcobj, version.HelmValues)
	if err != nil {
		serviceprovider.StatusProgressing(svcobj, conditionReasonError, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to reconcile HelmRelease: %w", err)
	}

	l.Info("Done reconciling ODG resource", "name", svcobj.Name)

	ociPhase, ociMsg := resourceStatus(ociRepo.Status.Conditions)
	hrPhase, hrMsg := resourceStatus(helmRel.Status.Conditions)
	svcobj.Status.Resources = []apiv1alpha1.ManagedResource{
		{
			TypedObjectReference: corev1.TypedObjectReference{
				APIGroup:  stringPtr(sourcev1.GroupVersion.Group),
				Kind:      "OCIRepository",
				Name:      OCIRepositoryName,
				Namespace: &tenantNamespace,
			},
			Phase:    ociPhase,
			Message:  ociMsg,
			Location: apiv1alpha1.PlatformCluster,
		},
		{
			TypedObjectReference: corev1.TypedObjectReference{
				APIGroup:  stringPtr(helmv2.GroupVersion.Group),
				Kind:      "HelmRelease",
				Name:      HelmReleaseName,
				Namespace: &tenantNamespace,
			},
			Phase:    hrPhase,
			Message:  hrMsg,
			Location: apiv1alpha1.PlatformCluster,
		},
	}

	if ociPhase == apiv1alpha1.Ready && hrPhase == apiv1alpha1.Ready {
		serviceprovider.StatusReady(svcobj)
	} else {
		serviceprovider.StatusProgressing(svcobj, "Reconciling", "Waiting for managed resources to become ready")
	}
	return ctrl.Result{}, nil
}

// Delete is called on every delete event
func (r *ODGReconciler) Delete(ctx context.Context, obj *apiv1alpha1.ODG, _ *apiv1alpha1.ProviderConfig, _ clusteraccess.ClusterContext) (ctrl.Result, error) {
	serviceprovider.StatusTerminating(obj)

	tenantNamespace, err := libutils.StableMCPNamespace(obj.Name, obj.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to determine stable namespace for ODG instance: %w", err)
	}
	obj.Status.Resources = managedResources(tenantNamespace, apiv1alpha1.Terminating)

	objects := []client.Object{
		&sourcev1.OCIRepository{
			ObjectMeta: metav1.ObjectMeta{Name: OCIRepositoryName, Namespace: tenantNamespace},
		},
		&helmv2.HelmRelease{
			ObjectMeta: metav1.ObjectMeta{Name: HelmReleaseName, Namespace: tenantNamespace},
		},
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

func createOciRepository(chartURL, secretName, chartVersion, namespace string) *sourcev1.OCIRepository {
	var secretRef *meta.LocalObjectReference
	if secretName != "" {
		secretRef = &meta.LocalObjectReference{Name: secretName}
	}

	return &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OCIRepositoryName,
			Namespace: namespace,
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

func (r *ODGReconciler) createOrUpdateOCIRepository(ctx context.Context, chartURL, chartVersion, secretName, namespace string) (*sourcev1.OCIRepository, error) {
	ociRepository := createOciRepository(chartURL, secretName, chartVersion, namespace)
	managedObj := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ociRepository.Name,
			Namespace: namespace,
		},
	}
	l := logf.FromContext(ctx)
	l.Info("creating OCI Repository", "object", ociRepository)
	if _, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), managedObj, func() error {
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

func (r *ODGReconciler) replicateWorkloadImagePullSecrets(ctx context.Context, workloadCluster *clusters.Cluster, secretName string) error {
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

	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: OdgSystemNamespace,
		},
	}

	if _, err := ctrl.CreateOrUpdate(ctx, workloadClient, targetSecret, func() error {
		targetSecret.Data = sourceSecret.Data
		targetSecret.Type = sourceSecret.Type
		return nil
	}); err != nil {
		return fmt.Errorf("failed to replicate chart pull secret %q to namespace %q: %w", secretName, OdgSystemNamespace, err)
	}

	return nil
}

func (r *ODGReconciler) createOrUpdateHelmRelease(ctx context.Context, namespace string, svcobj *apiv1alpha1.ODG, values *apiextensionsv1.JSON) (*helmv2.HelmRelease, error) {
	helmRelease, err := r.createHelmRelease(ctx, namespace, svcobj, values)
	if err != nil {
		return nil, fmt.Errorf("failed to create helm release: %w", err)
	}
	managedObj := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmRelease.Name,
			Namespace: namespace,
		},
	}
	l := logf.FromContext(ctx)
	l.Info("creating Helm Release", "object", managedObj)
	if _, err := ctrl.CreateOrUpdate(ctx, r.PlatformCluster.Client(), managedObj, func() error {
		managedObj.Spec = helmRelease.Spec
		return nil
	}); err != nil {
		return nil, err
	}

	return managedObj, nil
}

func (r *ODGReconciler) createHelmRelease(ctx context.Context, namespace string, svcobj *apiv1alpha1.ODG, helmValues *apiextensionsv1.JSON) (*helmv2.HelmRelease, error) {
	fluxConfigRef, err := r.getWorkloadFluxConfig(ctx, namespace, svcobj.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get FluxConfig: %w", err)
	}

	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HelmReleaseName,
			Namespace: namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			ReleaseName:      apiv1alpha1.DefaultReleaseName,
			Interval:         metav1.Duration{Duration: time.Minute},
			TargetNamespace:  OdgSystemNamespace,
			StorageNamespace: OdgSystemNamespace,
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
				Kind:      "OCIRepository",
				Name:      OCIRepositoryName,
				Namespace: namespace,
			},
			Values: helmValues,
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
// owns for an OCM instance, tagged with the given lifecycle phase.
func managedResources(tenantNamespace string, phase apiv1alpha1.InstancePhase) []apiv1alpha1.ManagedResource {
	return []apiv1alpha1.ManagedResource{
		{
			TypedObjectReference: corev1.TypedObjectReference{
				APIGroup:  stringPtr(sourcev1.GroupVersion.Group),
				Kind:      "OCIRepository",
				Name:      OCIRepositoryName,
				Namespace: stringPtr(tenantNamespace),
			},
			Phase:    phase,
			Location: apiv1alpha1.PlatformCluster,
		},
		{
			TypedObjectReference: corev1.TypedObjectReference{
				APIGroup:  stringPtr(helmv2.GroupVersion.Group),
				Kind:      "HelmRelease",
				Name:      HelmReleaseName,
				Namespace: stringPtr(tenantNamespace),
			},
			Phase:    phase,
			Location: apiv1alpha1.PlatformCluster,
		},
	}
}
