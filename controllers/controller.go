/*
Copyright 2022.

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

package controllers

import (
	"context"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type DeploymentController struct {
	client.Client
	*runtime.Scheme
}

const (
	deploymentControllerName = "deployment-controller"
	serviceAccountName       = "rbac-manager"
	roleName                 = "rbac-role"
	roleBindingName          = "rbac-role-binding"
)

var _ reconcile.Reconciler = &DeploymentController{}

func (p DeploymentController) Add(mgr manager.Manager) error {
	// Create a new Controller
	c, err := controller.New(deploymentControllerName, mgr,
		controller.Options{Reconciler: &DeploymentController{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}})
	if err != nil {
		logrus.Errorf("failed to create pod controller: %v", err)
		return err
	}

	// TODO Look for resources asking for permission to change a deployment with the label rbac-manager-request: deployment-name
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(
		v1.LabelSelector{
			MatchLabels: map[string]string{
				"rbac-management-operator.com/access": "nginx-deployment",
			},
		},
	)
	if err != nil {
		logrus.Errorf("Error creating label selector predicate: %v", err)
		return err
	}

	// Add a watch to Deployments containing that label
	err = c.Watch(
		&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForObject{}, labelSelectorPredicate)
	if err != nil {
		logrus.Errorf("error creating watch for deployments: %v", err)
		return err
	}

	err = c.Start(context.Background())

	return nil
}

//+kubebuilder:rbac:groups=core,resources=deployment,verbs=get;list;watch;create;update;patch;delete

func (p DeploymentController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logrus.Infof("Reconciling deployment %s", request.NamespacedName)

	var deployment appsv1.Deployment
	serviceAccount := corev1.ServiceAccount{
		ObjectMeta: v1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: request.Namespace,
		},
	}
	role := rbacv1.Role{
		ObjectMeta: v1.ObjectMeta{
			Name:      roleName,
			Namespace: request.Namespace,
		},
	}
	roleBinding := rbacv1.RoleBinding{
		ObjectMeta: v1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: request.Namespace,
		},
	}

	// TODO Deployment Logic

	// Get the requester deployment
	if err := p.Client.Get(ctx, request.NamespacedName, &deployment); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// TODO Service Account Logic

	// Check if service account is already assigned
	if deployment.Spec.Template.Spec.ServiceAccountName != serviceAccountName {

		// Check if the service account exists
		if err := p.Client.Get(ctx, client.ObjectKeyFromObject(&serviceAccount), &corev1.ServiceAccount{}); err != nil {
			if errors.IsNotFound(err) {
				// If not - create service account
				if err := p.Client.Create(ctx, &corev1.ServiceAccount{
					ObjectMeta: v1.ObjectMeta{
						Name:      serviceAccountName,
						Namespace: request.Namespace,
					},
				}); err != nil {
					return reconcile.Result{}, err
				}
			}
			return reconcile.Result{}, err
		}

		// Assign the service account to the requester
		deployment.Spec.Template.Spec.ServiceAccountName = serviceAccountName
		if err := p.Client.Update(ctx, &deployment); err != nil {
			return reconcile.Result{}, err
		}
	}

	// TODO Role Logic

	// Check if the role already exists
	if err := p.Client.Get(ctx, request.NamespacedName, &role); err != nil {
		if errors.IsNotFound(err) {
			// Create role
			if err := p.Client.Create(ctx, &rbacv1.Role{
				ObjectMeta: v1.ObjectMeta{
					Name:      roleName,
					Namespace: request.Namespace,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups:     []string{"apps"},
						Resources:     []string{"deployments"},
						Verbs:         []string{"get", "list", "watch", "create", "update", "patch", "delete"},
						ResourceNames: []string{deployment.Labels["rbac-management-operator.com/access"]},
					},
				},
			}); err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	// TODO Role Binding Logic

	// Check if the role binding already exists
	if err := p.Client.Get(ctx, request.NamespacedName, &roleBinding); err != nil {
		if errors.IsNotFound(err) {

			// Create role binding
			if err := p.Client.Create(ctx, &rbacv1.RoleBinding{
				ObjectMeta: v1.ObjectMeta{
					Name:      roleBindingName,
					Namespace: request.Namespace,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      rbacv1.ServiceAccountKind,
						Name:      serviceAccountName,
						Namespace: request.Namespace,
					},
				},
				RoleRef: rbacv1.RoleRef{
					Kind:     "Role",
					Name:     roleName,
					APIGroup: "rbac.authorization.k8s.io",
				},
			}); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
