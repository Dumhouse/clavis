package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ClavisSecretSpec selects secrets from one Clavis environment.
type ClavisSecretSpec struct {
	Server      string `json:"server"`
	Org         string `json:"org"`
	Project     string `json:"project"`
	Environment string `json:"environment"`

	// SecretName is the target Kubernetes Secret. Defaults to the resource name.
	SecretName string `json:"secretName,omitempty"`

	// TokenSecretRef points at a Secret containing a scoped Clavis token.
	TokenSecretRef corev1.SecretKeySelector `json:"tokenSecretRef"`

	// RefreshInterval defaults to five minutes.
	RefreshInterval *metav1.Duration `json:"refreshInterval,omitempty"`

	// RestartDeployments annotates these Deployments in the same namespace when
	// the resolved values change.
	RestartDeployments []string `json:"restartDeployments,omitempty"`

	// Keys limits the synced keys. Empty means all keys.
	Keys []string `json:"keys,omitempty"`
}

// ClavisSecretStatus reports the last successful sync.
type ClavisSecretStatus struct {
	LastSync   *metav1.Time       `json:"lastSync,omitempty"`
	Hash       string             `json:"hash,omitempty"`
	KeyCount   int                `json:"keyCount,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClavisSecret is the Schema for the clavissecrets API.
type ClavisSecret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ClavisSecretSpec   `json:"spec,omitempty"`
	Status            ClavisSecretStatus `json:"status,omitempty"`
}

// ClavisSecretList contains ClavisSecret items.
type ClavisSecretList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClavisSecret `json:"items"`
}

func (in *ClavisSecret) DeepCopyInto(out *ClavisSecret) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ClavisSecret) DeepCopy() *ClavisSecret {
	if in == nil {
		return nil
	}
	out := new(ClavisSecret)
	in.DeepCopyInto(out)
	return out
}

func (in *ClavisSecret) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ClavisSecretList) DeepCopyInto(out *ClavisSecretList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ClavisSecret, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ClavisSecretList) DeepCopy() *ClavisSecretList {
	if in == nil {
		return nil
	}
	out := new(ClavisSecretList)
	in.DeepCopyInto(out)
	return out
}

func (in *ClavisSecretList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ClavisSecretSpec) DeepCopyInto(out *ClavisSecretSpec) {
	*out = *in
	in.TokenSecretRef.DeepCopyInto(&out.TokenSecretRef)
	if in.RefreshInterval != nil {
		out.RefreshInterval = new(metav1.Duration)
		*out.RefreshInterval = *in.RefreshInterval
	}
	if in.RestartDeployments != nil {
		out.RestartDeployments = append([]string(nil), in.RestartDeployments...)
	}
	if in.Keys != nil {
		out.Keys = append([]string(nil), in.Keys...)
	}
}

func (in *ClavisSecretStatus) DeepCopyInto(out *ClavisSecretStatus) {
	*out = *in
	if in.LastSync != nil {
		out.LastSync = in.LastSync.DeepCopy()
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}
