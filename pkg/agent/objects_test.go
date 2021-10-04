package agent

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ptrutils "k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testName = "test-agent"

func TestClusterObjects(t *testing.T) {
	token := "test-token"
	testImage := "weaveworks/wkp-agent:v0.0.1"
	testNatsURL := "nats://192.168.0.2:4222"
	testAlertManagerURL := "https://alerts.example.com:9093"
	res := ClusterObjects(testName, token, testImage, testNatsURL, testAlertManagerURL)

	want := []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testName}},
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testName,
				Namespace: testName,
				Labels: map[string]string{
					"name": testName,
				},
			},
		},
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testName,
				Namespace: testName,
				Labels: map[string]string{
					"name": testName,
				},
			},
			Rules: clusterRoleRules,
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testName,
				Namespace: testName,
				Labels: map[string]string{
					"name": testName,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     testName,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      testName,
					Namespace: testName,
				},
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testName + "-token",
				Namespace: testName,
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"token": token,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testName,
				Namespace: testName,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						k8sAppLabel: testName,
					},
				},
				Replicas: ptrutils.Int32Ptr(1),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							k8sAppLabel: testName,
						},
					},
					Spec: corev1.PodSpec{
						ServiceAccountName: testName,
						Containers: []corev1.Container{
							{
								Name:  testName,
								Image: testImage,
								Args:  []string{"watch", "--nats-url=" + testNatsURL},
								Env: []corev1.EnvVar{
									{
										Name: "WKP_AGENT_TOKEN",
										ValueFrom: &corev1.EnvVarSource{
											SecretKeyRef: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: secretName(testName),
												},
												Key: secretTokenKey,
											},
										},
									},
								},
							},
							{
								Name:  alertManagerAgentName,
								Image: testImage,
								Args:  []string{"agent-server", "--nats-url=" + testNatsURL, "--alertmanager-url=" + testAlertManagerURL},
								Env: []corev1.EnvVar{
									{
										Name: "WKP_AGENT_TOKEN",
										ValueFrom: &corev1.EnvVarSource{
											SecretKeyRef: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: secretName(testName),
												},
												Key: secretTokenKey,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if diff := cmp.Diff(want, res); diff != "" {
		t.Fatalf("failed to generate cluster objects:\n%s", diff)
	}
}
