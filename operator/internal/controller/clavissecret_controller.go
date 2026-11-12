// Package controller reconciles ClavisSecret resources into Kubernetes Secrets.
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clavisv1alpha1 "github.com/dumhouse/clavis/operator/api/v1alpha1"
)

const restartedAtAnnotation = "clavis.dumhouse.github.io/restartedAt"

// ClavisSecretReconciler reconciles ClavisSecret resources.
type ClavisSecretReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	HTTPClient *http.Client
}

// +kubebuilder:rbac:groups=clavis.dumhouse.github.io,resources=clavissecrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=clavis.dumhouse.github.io,resources=clavissecrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch

// Reconcile fetches resolved secrets and writes the target Secret.
func (r *ClavisSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var resource clavisv1alpha1.ClavisSecret
	if err := r.Get(ctx, req.NamespacedName, &resource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	values, err := r.resolve(ctx, &resource)
	if err != nil {
		r.setCondition(ctx, &resource, metav1.ConditionFalse, "ResolveFailed", err.Error())
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}
	hash := hashValues(values)
	targetName := resource.Spec.SecretName
	if targetName == "" {
		targetName = resource.Name
	}
	if err := r.writeSecret(ctx, &resource, targetName, values); err != nil {
		r.setCondition(ctx, &resource, metav1.ConditionFalse, "WriteFailed", err.Error())
		return ctrl.Result{}, err
	}
	changed := resource.Status.Hash != hash
	if changed && len(resource.Spec.RestartDeployments) > 0 {
		if err := r.restartDeployments(ctx, &resource); err != nil {
			logger.Error(err, "restart deployments failed")
			r.setCondition(ctx, &resource, metav1.ConditionFalse, "RestartFailed", err.Error())
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
	}
	now := metav1.Now()
	resource.Status.LastSync = &now
	resource.Status.Hash = hash
	resource.Status.KeyCount = len(values)
	r.setCondition(ctx, &resource, metav1.ConditionTrue, "Synced", fmt.Sprintf("synced %d keys", len(values)))
	if err := r.Status().Update(ctx, &resource); err != nil {
		return ctrl.Result{}, err
	}

	interval := 5 * time.Minute
	if resource.Spec.RefreshInterval != nil && resource.Spec.RefreshInterval.Duration > 0 {
		interval = resource.Spec.RefreshInterval.Duration
	}
	return ctrl.Result{RequeueAfter: interval}, nil
}

type resolveResponse struct {
	Secrets []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"secrets"`
}

func (r *ClavisSecretReconciler) resolve(ctx context.Context, resource *clavisv1alpha1.ClavisSecret) (map[string]string, error) {
	var tokenSecret corev1.Secret
	selector := resource.Spec.TokenSecretRef
	if selector.Name == "" || selector.Key == "" {
		return nil, fmt.Errorf("spec.tokenSecretRef.name and key are required")
	}
	if err := r.Get(ctx, types.NamespacedName{Namespace: resource.Namespace, Name: selector.Name}, &tokenSecret); err != nil {
		return nil, fmt.Errorf("read token secret: %w", err)
	}
	token := strings.TrimSpace(string(tokenSecret.Data[selector.Key]))
	if token == "" {
		return nil, fmt.Errorf("token secret %s/%s key %s is empty", resource.Namespace, selector.Name, selector.Key)
	}
	base := strings.TrimRight(resource.Spec.Server, "/")
	endpoint := base + "/api/v1/orgs/" + url.PathEscape(resource.Spec.Org) +
		"/projects/" + url.PathEscape(resource.Spec.Project) +
		"/environments/" + url.PathEscape(resource.Spec.Environment) + "/secrets:resolve"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Clavis: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Clavis returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var resolved resolveResponse
	if err := json.Unmarshal(body, &resolved); err != nil {
		return nil, fmt.Errorf("decode Clavis response: %w", err)
	}
	allowed := map[string]bool{}
	for _, key := range resource.Spec.Keys {
		allowed[key] = true
	}
	values := make(map[string]string, len(resolved.Secrets))
	for _, secret := range resolved.Secrets {
		if len(allowed) > 0 && !allowed[secret.Key] {
			continue
		}
		values[secret.Key] = secret.Value
	}
	return values, nil
}

func (r *ClavisSecretReconciler) writeSecret(ctx context.Context, resource *clavisv1alpha1.ClavisSecret, name string, values map[string]string) error {
	data := make(map[string][]byte, len(values))
	for key, value := range values {
		data[key] = []byte(value)
	}
	var target corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Namespace: resource.Namespace, Name: name}, &target)
	if apierrors.IsNotFound(err) {
		target = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: resource.Namespace},
			Type:       corev1.SecretTypeOpaque,
			Data:       data,
		}
		if err := controllerutil.SetControllerReference(resource, &target, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, &target)
	}
	if err != nil {
		return err
	}
	if equalData(target.Data, data) {
		return nil
	}
	target.Data = data
	if err := controllerutil.SetControllerReference(resource, &target, r.Scheme); err != nil {
		return err
	}
	return r.Update(ctx, &target)
}

func (r *ClavisSecretReconciler) restartDeployments(ctx context.Context, resource *clavisv1alpha1.ClavisSecret) error {
	stamp := time.Now().UTC().Format(time.RFC3339)
	for _, name := range resource.Spec.RestartDeployments {
		var deployment appsv1.Deployment
		if err := r.Get(ctx, types.NamespacedName{Namespace: resource.Namespace, Name: name}, &deployment); err != nil {
			return fmt.Errorf("get deployment %s: %w", name, err)
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations[restartedAtAnnotation] = stamp
		if err := r.Update(ctx, &deployment); err != nil {
			return fmt.Errorf("update deployment %s: %w", name, err)
		}
	}
	return nil
}

func (r *ClavisSecretReconciler) setCondition(ctx context.Context, resource *clavisv1alpha1.ClavisSecret, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&resource.Status.Conditions, metav1.Condition{
		Type: "Ready", Status: status, Reason: reason, Message: message,
		ObservedGeneration: resource.Generation,
	})
	if err := r.Status().Update(ctx, resource); err != nil {
		log.FromContext(ctx).Error(err, "update status failed")
	}
}

func hashValues(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		fmt.Fprintf(h, "%s=%s\x00", key, values[key])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func equalData(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		other, ok := b[key]
		if !ok || string(other) != string(value) {
			return false
		}
	}
	return true
}

// SetupWithManager registers the reconciler.
func (r *ClavisSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clavisv1alpha1.ClavisSecret{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
