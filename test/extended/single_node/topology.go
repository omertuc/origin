package single_node

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "github.com/openshift/api/config/v1"
	exutil "github.com/openshift/origin/test/extended/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	e2e "k8s.io/kubernetes/test/e2e/framework"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	"strings"
)

func getOpenshiftNamespaces(f *e2e.Framework) []corev1.Namespace {
	list, err := f.ClientSet.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	var openshiftNamespaces []corev1.Namespace
	for _, namespace := range list.Items {
		if strings.HasPrefix(namespace.Name, "openshift-") {
			openshiftNamespaces = append(openshiftNamespaces, namespace)
		}
	}

	return openshiftNamespaces
}

func getNamespaceDeployments(f *e2e.Framework, namespace corev1.Namespace) []appsv1.Deployment {
	list, err := f.ClientSet.AppsV1().Deployments(namespace.Name).List(context.Background(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	return list.Items
}

func getNamespaceStatefulSets(f *e2e.Framework, namespace corev1.Namespace) []appsv1.StatefulSet {
	list, err := f.ClientSet.AppsV1().StatefulSets(namespace.Name).List(context.Background(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())

	return list.Items
}

func getTopologies(f *e2e.Framework) (controlPlaneTopology, infraTopology v1.TopologyMode) {
	oc := exutil.NewCLIWithFramework(f)
	infra, err := oc.AdminConfigClient().ConfigV1().Infrastructures().Get(context.Background(),
		"cluster", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())

	return infra.Status.ControlPlaneTopology, infra.Status.InfrastructureTopology
}

// isInfrastructureStatefulSet decides if a StatefulSet is considered "infrastructure" or
// "control plane" by comparing it against a known list
func isInfrastructureStatefulSet(statefulSet appsv1.StatefulSet) bool {
	infrastructureNamespaces := map[string][]string{
		// No known OpenShift StatefulSets are considered "infrastructure" for now
	}

	namespaceInfraStatefulSets, ok := infrastructureNamespaces[statefulSet.Namespace]

	if !ok {
		return false
	}

	for _, infraStatefulSetName := range namespaceInfraStatefulSets {
		if statefulSet.Name == infraStatefulSetName {
			return true
		}
	}

	return false
}

// isInfrastructureDeployment decides if a deployment is considered "infrastructure" or
// "control plane" by comparing it against a known list
func isInfrastructureDeployment(deployment appsv1.Deployment) bool {
	infrastructureNamespaces := map[string][]string{
		"openshift-ingress": {
			"router-default",
		},
	}

	namespaceInfraDeployments, ok := infrastructureNamespaces[deployment.Namespace]

	if !ok {
		return false
	}

	for _, infraDeploymentName := range namespaceInfraDeployments {
		if deployment.Name == infraDeploymentName {
			return true
		}
	}

	return false
}

func validateReplicas(name, namespace string, replicas int, failureAllowed bool) {
	if !failureAllowed {
		Expect(replicas).To(Equal(1),
			"%s in %s namespace has wrong number of replicas", name, namespace)
	} else {
		if replicas == 1 {
			t := GinkgoT()
			t.Logf("%s in namespace %s has one replica, consider taking it off the topology allow-list",
				name, namespace)
		}
	}
}

func validateStatefulSetReplicas(statefulSet appsv1.StatefulSet, controlPlaneTopology,
	infraTopology v1.TopologyMode, failureAllowed bool) {
	if isInfrastructureStatefulSet(statefulSet) {
		if infraTopology != v1.SingleReplicaTopologyMode {
			return
		}
	} else if controlPlaneTopology != v1.SingleReplicaTopologyMode {
		return
	}

	Expect(statefulSet.Spec.Replicas).ToNot(BeNil())

	validateReplicas(statefulSet.Name, statefulSet.Namespace, int(*statefulSet.Spec.Replicas), failureAllowed)
}

func validateDeploymentReplicas(deployment appsv1.Deployment,
	controlPlaneTopology, infraTopology v1.TopologyMode, failureAllowed bool) {
	if isInfrastructureDeployment(deployment) {
		if infraTopology != v1.SingleReplicaTopologyMode {
			return
		}
	} else if controlPlaneTopology != v1.SingleReplicaTopologyMode {
		return
	}

	Expect(deployment.Spec.Replicas).ToNot(BeNil())

	validateReplicas(deployment.Name, deployment.Namespace, int(*deployment.Spec.Replicas), failureAllowed)
}

func isAllowedToFail(name, namespace string) bool {
	// allowedToFail is a list of deployments and statefulsets that currently have 2 replicas
	// even in single-replica topology deployments, because their operator has yet to be made
	// aware of the new API. We will slowly remove deployments from this list once their operators
	// have been made aware, until this list is empty and this function will be removed.
	allowedToFail := map[string][]string{
		"openshift-authentication": {
			// Deployments
			"oauth-openshift",
		},
		"openshift-console": {
			// Deployments
			"console",
			"downloads",
		},
		"openshift-image-registry": {
			"image-registry",
		},
		"openshift-monitoring": {
			// Deployments
			"prometheus-adapter",
			"thanos-querier",

			// StatefulSets
			"alertmanager-main",
			"prometheus-k8s",
		},
		"openshift-operator-lifecycle-manager": {
			// Deployments
			"packageserver",
		},
	}

	namespaceAllowedToFailDeployments, ok := allowedToFail[namespace]

	if !ok {
		return false
	}

	for _, allowedToFailDeploymentName := range namespaceAllowedToFailDeployments {
		if name == allowedToFailDeploymentName {
			return true
		}
	}

	return false
}

func isDeploymentAllowedToFail(deployment appsv1.Deployment) bool {
	return isAllowedToFail(deployment.Name, deployment.Namespace)
}

func isStatefulSetAllowedToFail(statefulSet appsv1.StatefulSet) bool {
	return isAllowedToFail(statefulSet.Name, statefulSet.Namespace)
}

var _ = Describe("[sig-arch] Cluster topology single node tests", func() {
	f := e2e.NewDefaultFramework("single-node")

	It("Verify that OpenShift components deploy one replica in SingleReplica topology mode", func() {
		controlPlaneTopology, infraTopology := getTopologies(f)

		if controlPlaneTopology != v1.SingleReplicaTopologyMode && infraTopology != v1.SingleReplicaTopologyMode {
			e2eskipper.Skipf("Test is only relevant for single replica topologies")
		}

		for _, namespace := range getOpenshiftNamespaces(f) {
			for _, deployment := range getNamespaceDeployments(f, namespace) {
				validateDeploymentReplicas(deployment,
					controlPlaneTopology, infraTopology, isDeploymentAllowedToFail(deployment))
			}

			for _, statefulSet := range getNamespaceStatefulSets(f, namespace) {
				validateStatefulSetReplicas(statefulSet,
					controlPlaneTopology, infraTopology, isStatefulSetAllowedToFail(statefulSet))
			}
		}
	})
})
