/*
Copyright 2021 The cert-manager Authors.

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

package test

import (
	"context"
	"crypto/x509"
	"time"

	apiutil "github.com/cert-manager/cert-manager/pkg/api/util"
	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	policyapi "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	"github.com/cert-manager/approver-policy/pkg/internal/controllers"
	"github.com/cert-manager/approver-policy/pkg/registry"
	"github.com/cert-manager/cert-manager/test/unit/gen"
)

// waitForApproval will wait for the CertificateRequest, given by namespace and
// name, to become in an Approved state.
func waitForApproval(ctx context.Context, cl client.Client, ns, name string) {
	Eventually(func() bool {
		cr := new(cmapi.CertificateRequest)
		Eventually(func() error {
			return cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, cr)
		}).WithTimeout(time.Second * 10).WithPolling(time.Millisecond * 10).Should(BeNil())
		return apiutil.CertificateRequestIsApproved(cr)
	}).WithTimeout(time.Second*10).WithPolling(time.Millisecond*10).Should(BeTrue(), "expected approval")
}

// waitForDenial will wait for the CertificateRequest, given by namespace and
// name, to become in an Denied state.
func waitForDenial(ctx context.Context, cl client.Client, ns, name string) {
	Eventually(func() bool {
		cr := new(cmapi.CertificateRequest)
		Eventually(func() error {
			return cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, cr)
		}).WithTimeout(time.Second * 10).WithPolling(time.Millisecond * 10).Should(BeNil())
		return apiutil.CertificateRequestIsDenied(cr)
	}).WithTimeout(time.Second*10).WithPolling(time.Millisecond*10).Should(BeTrue(), "expected denial")
}

// waitForNoApproveOrDeny will wait a reasonable amount of time (3 seconds) for
// the CertificateRequest to be marked as neither Approved or Denied.
func waitForNoApproveOrDeny(ctx context.Context, cl client.Client, ns, name string) {
	Consistently(func() bool {
		cr := new(cmapi.CertificateRequest)
		Eventually(func() error {
			return cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, cr)
		}).WithTimeout(time.Second * 10).WithPolling(time.Millisecond * 10).Should(BeNil())
		return apiutil.CertificateRequestIsApproved(cr) || apiutil.CertificateRequestIsDenied(cr)
	}).WithTimeout(time.Second*10).WithPolling(time.Millisecond*10).Should(BeFalse(), "expected neither approved not denied")
}

// waitForReady will wait for the CertificateRequestPolicy, given by name, to
// become in an Ready state. Will ensure the Ready condition has the same
// observed Generation as the object's Generation.
func waitForReady(ctx context.Context, cl client.Client, name string) {
	Eventually(func() bool {
		var policy policyapi.CertificateRequestPolicy
		Eventually(func() error {
			return cl.Get(ctx, client.ObjectKey{Name: name}, &policy)
		}).WithTimeout(time.Second * 10).WithPolling(time.Millisecond * 10).Should(BeNil())
		for _, condition := range policy.Status.Conditions {
			if condition.ObservedGeneration != policy.Generation {
				return false
			}
			if condition.Type == policyapi.CertificateRequestPolicyConditionReady && condition.Status == corev1.ConditionTrue {
				return true
			}
		}
		return false
	}).WithTimeout(time.Second*10).WithPolling(time.Millisecond*10).Should(BeTrue(), "expected policy to become ready")
}

// waitForNotReady will wait for the CertificateRequestPolicy, given by name,
// become in an Not-Ready state. Will ensure the Ready condition has the same
// observed Generation as the object's Generation.
func waitForNotReady(ctx context.Context, cl client.Client, name string) {
	Eventually(func() bool {
		var policy policyapi.CertificateRequestPolicy
		Eventually(func() error {
			return cl.Get(ctx, client.ObjectKey{Name: name}, &policy)
		}).WithTimeout(time.Second * 10).WithPolling(time.Millisecond * 10).Should(BeNil())
		for _, condition := range policy.Status.Conditions {
			if condition.ObservedGeneration != policy.Generation {
				return false
			}
			if condition.Type == policyapi.CertificateRequestPolicyConditionReady && condition.Status == corev1.ConditionFalse {
				return true
			}
		}
		return false
	}).WithTimeout(time.Second*10).WithPolling(time.Millisecond*10).Should(BeTrue(), "expected policy to become not-ready")
}

// startControllers will create a test Namespace and start the approver-policy
// controllers and ensure they are active and ready. This function is intended
// to be run in a JustBefore block before any test logic has started. The
// created namespace as well as a shutdown function to stop the controllers is
// returned.
func startControllers(registry *registry.Registry) (context.Context, func(), corev1.Namespace) {
	ctx, cancel := context.WithCancel(context.Background())

	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-policy-",
		},
	}
	Expect(env.AdminClient.Create(ctx, &namespace)).NotTo(HaveOccurred())
	By("Created Policy Namespace: " + namespace.Name)

	shutdown := func() {
		By("Deleting all CertificateRequests after test")
		env.AdminClient.DeleteAllOf(ctx, new(cmapi.CertificateRequest))

		By("Deleting all CertificateRequestPolicies after test")
		env.AdminClient.DeleteAllOf(ctx, new(policyapi.CertificateRequestPolicy))

		By("Deleting Policy Namespace: " + namespace.Name)
		env.AdminClient.Delete(ctx, &namespace)

		cancel()
	}

	log := klogr.New().WithName("testing")
	mgr, err := ctrl.NewManager(env.Config, ctrl.Options{
		Scheme:                        policyapi.GlobalScheme,
		LeaderElection:                true,
		MetricsBindAddress:            "0",
		LeaderElectionNamespace:       namespace.Name,
		LeaderElectionID:              "cert-manager-approver-policy",
		LeaderElectionReleaseOnCancel: true,
		Logger:                        log,
	})
	Expect(err).NotTo(HaveOccurred())

	Expect(controllers.AddControllers(ctx, controllers.Options{
		Log: log, Manager: mgr,
		Evaluators: registry.Evaluators(), Reconcilers: registry.Reconcilers(),
	})).NotTo(HaveOccurred())

	By("Running Policy controller")
	go mgr.Start(ctx)

	By("Waiting for Leader Election")
	<-mgr.Elected()

	By("Waiting for Informers to Sync")
	Expect(mgr.GetCache().WaitForCacheSync(ctx)).Should(BeTrue())

	return ctx, shutdown, namespace
}

// bindUserToUseCertificateRequestPolicies creates an RBAC Role and RoleBinding
// that binds to the given user to use the CertificateRequestPolicies in the
// given Namespace. The name of the Role and RoleBinding is returned, which
// should be deleted after the test has completed by the consumer.
func bindUserToUseCertificateRequestPolicies(ctx context.Context, cl client.Client, ns, username string, policyNames ...string) string {
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-policy-use-",
			Namespace:    ns,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"policy.cert-manager.io"},
				Resources:     []string{"certificaterequestpolicies"},
				Verbs:         []string{"use"},
				ResourceNames: policyNames,
			},
		},
	}
	Expect(cl.Create(ctx, &role)).NotTo(HaveOccurred())

	binding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      role.Name,
			Namespace: ns,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				Name:     username,
				APIGroup: "rbac.authorization.k8s.io",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     role.Name,
		},
	}
	Expect(cl.Create(ctx, &binding)).NotTo(HaveOccurred())

	return role.Name
}

// bindUserToCreateCertificateRequest creates an RBAC Role and RoleBinding that
// binds to the given user to create CertificateRequests in the given
// Namespace. The name of the Role and RoleBinding is returned, which should be
// deleted after the test has completed by the consumer.
func bindUserToCreateCertificateRequest(ctx context.Context, cl client.Client, ns, username string) string {
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cr-create-",
			Namespace:    ns,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"cert-manager.io"},
				Resources: []string{"certificaterequests"},
				Verbs:     []string{"create"},
			},
		},
	}
	Expect(cl.Create(ctx, &role)).NotTo(HaveOccurred())

	binding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      role.Name,
			Namespace: ns,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				Name:     username,
				APIGroup: "rbac.authorization.k8s.io",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     role.Name,
		},
	}
	Expect(cl.Create(ctx, &binding)).NotTo(HaveOccurred())

	return role.Name
}

// deleteRoleAndRoleBindings deletes the Role and RoleBindings that have the
// given name.
func deleteRoleAndRoleBindings(ctx context.Context, cl client.Client, ns string, names ...string) {
	for _, name := range names {
		Expect(env.AdminClient.Delete(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}})).ToNot(HaveOccurred())
		Expect(env.AdminClient.Delete(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}})).ToNot(HaveOccurred())
	}
}

// createCertificateRequest will create a CertificateRequest with an X.509
// certificate request using and RSA key, which includes the modifiers
// provided. The name of the created CertificateRequest is returned.
func createCertificateRequest(ctx context.Context, cl client.Client, ns string, csrMod gen.CSRModifier, crMods ...gen.CertificateRequestModifier) string {
	csr, _, err := gen.CSR(x509.RSA, csrMod)
	Expect(err).ToNot(HaveOccurred())

	cr := gen.CertificateRequest("", append(crMods,
		func(cr *cmapi.CertificateRequest) {
			cr.GenerateName = "test-"
		},
		gen.SetCertificateRequestCSR(csr),
		gen.SetCertificateRequestNamespace(ns),
	)...)
	Expect(cl.Create(ctx, cr)).ToNot(HaveOccurred())

	return cr.Name
}
