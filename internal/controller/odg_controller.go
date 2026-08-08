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
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/opencontrolplane-runtime/pkg/serviceprovider"
	clusteraccess "github.com/openmcp-project/opencontrolplane-runtime/pkg/serviceprovider/clusteraccess"
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
}

// CreateOrUpdate is called on every add or update event
func (r *ODGReconciler) CreateOrUpdate(ctx context.Context, svcobj *apiv1alpha1.ODG, _ *apiv1alpha1.ProviderConfig, clusters clusteraccess.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	serviceprovider.StatusProgressing(svcobj, "Reconciling", "Reconcile in progress")
	managedObj := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foos.example.domain",
		},
	}
	if _, err := ctrl.CreateOrUpdate(ctx, clusters.MCPCluster.Client(), managedObj, func() error {
		managedObj.Spec = fooCRD().Spec
		return nil
	}); err != nil {
		l.Error(err, "createOrUpdate failed")
		return ctrl.Result{}, err
	}
	serviceprovider.StatusReady(svcobj)
	return ctrl.Result{}, nil
}

// Delete is called on every delete event
func (r *ODGReconciler) Delete(ctx context.Context, obj *apiv1alpha1.ODG, _ *apiv1alpha1.ProviderConfig, clusters clusteraccess.ClusterContext) (ctrl.Result, error) {
	l := logf.FromContext(ctx)
	serviceprovider.StatusTerminating(obj)
	managedObj := fooCRD()
	if err := clusters.MCPCluster.Client().Delete(ctx, managedObj); client.IgnoreNotFound(err) != nil {
		l.Error(err, "delete object failed")
		return ctrl.Result{}, err
	}
	if err := clusters.MCPCluster.Client().Get(ctx, client.ObjectKeyFromObject(managedObj), managedObj); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	// object still exists
	return ctrl.Result{
		RequeueAfter: time.Second * 10,
	}, nil
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

func fooCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foos.example.domain",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.domain",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"foo": {Type: "string"},
									},
								},
							},
						},
					},
				},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "foos",
				Singular: "foo",
				Kind:     "Foo",
				ListKind: "FooList",
			},
		},
	}
}
