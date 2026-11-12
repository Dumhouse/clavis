// Package v1alpha1 contains the ClavisSecret API.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion is the API group and version.
var GroupVersion = schema.GroupVersion{Group: "clavis.dumhouse.github.io", Version: "v1alpha1"}

// SchemeBuilder registers the API types.
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

// AddToScheme adds the types to a scheme.
var AddToScheme = SchemeBuilder.AddToScheme

func init() {
	SchemeBuilder.Register(&ClavisSecret{}, &ClavisSecretList{})
}

var _ = runtime.Object(nil)
