package agent

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ptrutils "k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	k8sAppLabel           = "app.kubernetes.io/name"
	secretTokenKey        = "token"
	alertManagerAgentName = "alertmanager-agent"
)

var clusterRoleRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{"*"},
		Resources: []string{"*"},
		Verbs:     []string{"get", "watch", "list"},
	},
	{
		NonResourceURLs: []string{"*"},
		Verbs:           []string{"get"},
	},
}

// ClusterObjects creates and returns the set of objects needed to deploy the
// agent into a cluster.
func ClusterObjects(name, secret, image, natsURL, alertManagerURL string) []client.Object {
	return []client.Object{
		makeNamespace(name),
		makeServiceAccount(name),
		makeClusterRole(name),
		makeClusterRoleBinding(name),
		makeSecret(name, secret),
		makeDeployment(name, image, natsURL, alertManagerURL),
	}
}

func makeNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func makeServiceAccount(name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: labeledObjectMeta(name),
	}
}

func makeClusterRole(name string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: labeledObjectMeta(name),
		Rules:      clusterRoleRules,
	}
}

func makeClusterRoleBinding(name string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: labeledObjectMeta(name),
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      name,
				Namespace: name,
			},
		},
	}
}

func makeSecret(name, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName(name),
			Namespace: name,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			secretTokenKey: token,
		},
	}
}

func makeDeployment(name, image, natsURL, alertManagerURL string) *appsv1.Deployment {
	agentTokenEnvVar := corev1.EnvVar{
		Name: "WKP_AGENT_TOKEN",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName(name),
				},
				Key: secretTokenKey,
			},
		},
	}

	return &appsv1.Deployment{
		ObjectMeta: objectMeta(name),
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					k8sAppLabel: name,
				},
			},
			Replicas: ptrutils.Int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						k8sAppLabel: name,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: name,
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: image,
							Args:  []string{"watch", "--nats-url=" + natsURL},
							Env:   []corev1.EnvVar{agentTokenEnvVar},
						},
						{
							Name:  alertManagerAgentName,
							Image: image,
							Args:  []string{"agent-server", "--nats-url=" + natsURL, "--alertmanager-url=" + alertManagerURL},
							Env:   []corev1.EnvVar{agentTokenEnvVar},
						},
					},
				},
			},
		},
	}
}

func labeledObjectMeta(name string) metav1.ObjectMeta {
	return objectMeta(name, func(om *metav1.ObjectMeta) {
		om.Labels = map[string]string{
			"name": name,
		}
	})
}

func objectMeta(name string, opts ...func(*metav1.ObjectMeta)) metav1.ObjectMeta {
	om := metav1.ObjectMeta{
		Name:      name,
		Namespace: name,
	}
	for _, o := range opts {
		o(&om)
	}

	return om
}

func secretName(name string) string {
	return name + "-token"
}
