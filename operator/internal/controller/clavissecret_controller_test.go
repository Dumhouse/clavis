package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clavisv1alpha1 "github.com/dumhouse/clavis/operator/api/v1alpha1"
)

func TestReconcileWritesSecretAndRestartsDeployment(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgres://one", "FLAG": "on"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer clv_mt_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"secrets": []map[string]string{
				{"key": "DATABASE_URL", "value": values["DATABASE_URL"]},
				{"key": "FLAG", "value": values["FLAG"]},
			},
		})
	}))
	defer server.Close()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = clavisv1alpha1.AddToScheme(scheme)

	resource := &clavisv1alpha1.ClavisSecret{
		ObjectMeta: metav1.ObjectMeta{Name: "app-secrets", Namespace: "default", UID: types.UID("uid-1")},
		Spec: clavisv1alpha1.ClavisSecretSpec{
			Server: server.URL, Org: "acme", Project: "web-app", Environment: "dev",
			TokenSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "clavis-token"},
				Key:                  "token",
			},
			RestartDeployments: []string{"app"},
		},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "clavis-token", Namespace: "default"},
		Data:       map[string][]byte{"token": []byte("clv_mt_test")},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example/app"}}},
			},
		},
	}
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&clavisv1alpha1.ClavisSecret{}).
		WithObjects(resource, tokenSecret, deployment).
		Build()
	reconciler := &ClavisSecretReconciler{Client: client, Scheme: scheme, HTTPClient: server.Client()}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-secrets", Namespace: "default"}}); err != nil {
		t.Fatal(err)
	}
	var secret corev1.Secret
	if err := client.Get(context.Background(), types.NamespacedName{Name: "app-secrets", Namespace: "default"}, &secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["DATABASE_URL"]) != "postgres://one" || string(secret.Data["FLAG"]) != "on" {
		t.Fatalf("secret data = %#v", secret.Data)
	}
	var updated appsv1.Deployment
	if err := client.Get(context.Background(), types.NamespacedName{Name: "app", Namespace: "default"}, &updated); err != nil {
		t.Fatal(err)
	}
	firstStamp := updated.Spec.Template.Annotations[restartedAtAnnotation]
	if firstStamp == "" {
		t.Fatal("deployment was not restarted")
	}

	// A second reconcile with unchanged values must not restart again.
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-secrets", Namespace: "default"}}); err != nil {
		t.Fatal(err)
	}
	if err := client.Get(context.Background(), types.NamespacedName{Name: "app", Namespace: "default"}, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Template.Annotations[restartedAtAnnotation] != firstStamp {
		t.Fatal("unchanged values restarted the deployment again")
	}

	var after clavisv1alpha1.ClavisSecret
	if err := client.Get(context.Background(), types.NamespacedName{Name: "app-secrets", Namespace: "default"}, &after); err != nil {
		t.Fatal(err)
	}
	if after.Status.Hash == "" || after.Status.KeyCount != 2 {
		t.Fatalf("status = %+v", after.Status)
	}
}
