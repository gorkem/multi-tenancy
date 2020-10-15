package validators

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/multi-tenancy/incubator/hnc/internal/forest"
	"sigs.k8s.io/multi-tenancy/incubator/hnc/internal/foresttest"

	api "sigs.k8s.io/multi-tenancy/incubator/hnc/api/v1alpha2"
)

var (
	// This mapping is used to implement a fake grTranslator with GVKFor() method.
	gr2gvk = map[schema.GroupResource]schema.GroupVersionKind{
		{Group: api.RBACGroup, Resource: api.RoleResource}:        {Group: api.RBACGroup, Version: "v1", Kind: api.RoleKind},
		{Group: api.RBACGroup, Resource: api.RoleBindingResource}: {Group: api.RBACGroup, Version: "v1", Kind: api.RoleBindingKind},
		{Group: "", Resource: "secrets"}:                          {Group: "", Version: "v1", Kind: "Secret"},
		{Group: "", Resource: "resourcequotas"}:                   {Group: "", Version: "v1", Kind: "ResourceQuota"},
	}
)

func TestDeletingConfigObject(t *testing.T) {
	t.Run("Delete config object", func(t *testing.T) {
		g := NewGomegaWithT(t)
		req := admission.Request{
			AdmissionRequest: v1beta1.AdmissionRequest{
				Operation: v1beta1.Delete,
				Name:      api.HNCConfigSingleton,
			}}
		config := &HNCConfig{}

		got := config.Handle(context.Background(), req)

		logResult(t, got.AdmissionResponse.Result)
		g.Expect(got.AdmissionResponse.Allowed).Should(BeFalse())
	})
}

func TestDeletingOtherObject(t *testing.T) {
	t.Run("Delete config object", func(t *testing.T) {
		g := NewGomegaWithT(t)
		req := admission.Request{
			AdmissionRequest: v1beta1.AdmissionRequest{
				Operation: v1beta1.Delete,
				Name:      "other",
			}}
		config := &HNCConfig{}

		got := config.Handle(context.Background(), req)

		logResult(t, got.AdmissionResponse.Result)
		g.Expect(got.AdmissionResponse.Allowed).Should(BeTrue())
	})
}

func TestRBACTypes(t *testing.T) {
	f := forest.NewForest()
	config := &HNCConfig{translator: fakeGRTranslator{}, Forest: f}

	tests := []struct {
		name    string
		configs []api.TypeSynchronizationSpec
		allow   bool
	}{
		{
			name: "Correct RBAC config with Propagate mode",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
			},
			allow: true,
		},
		{
			name: "Correct RBAC config with unset mode",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource},
			},
			allow: true,
		},
		{
			name: "Missing role",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
			},
			allow: false,
		}, {
			name: "Missing rolebinding",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
			},
			allow: false,
		}, {
			name: "Incorrect role mode",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Ignore"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
			},
			allow: false,
		}, {
			name: "Incorrect rolebinding mode",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Ignore"},
			},
			allow: false,
		}, {
			name: "Duplicate RBAC types with different modes",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleResource},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
			},
			allow: false,
		},
		{
			name: "Duplicate RBAC types with the same mode",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
			},
			allow: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			c := &api.HNCConfiguration{Spec: api.HNCConfigurationSpec{Types: tc.configs}}
			c.Name = api.HNCConfigSingleton

			got := config.handle(context.Background(), c)

			logResult(t, got.AdmissionResponse.Result)
			g.Expect(got.AdmissionResponse.Allowed).Should(Equal(tc.allow))
		})
	}
}

func TestNonRBACTypes(t *testing.T) {
	f := fakeGRTranslator{"crontabs"}
	tests := []struct {
		name      string
		configs   []api.TypeSynchronizationSpec
		validator fakeGRTranslator
		allow     bool
	}{
		{
			name: "Correct Non-RBAC types config",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
				{Group: "", Resource: "secrets", Mode: "Ignore"},
				{Group: "", Resource: "resourcequotas"},
			},
			validator: f,
			allow:     true,
		},
		{
			name: "Resource does not exist",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
				// "crontabs" resource does not exist in ""
				{Group: "", Resource: "crontabs", Mode: "Ignore"},
			},
			validator: f,
			allow:     false,
		}, {
			name: "Duplicate types with different modes",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
				{Group: "", Resource: "secrets", Mode: "Ignore"},
				{Group: "", Resource: "secrets", Mode: "Propagate"},
			},
			validator: f,
			allow:     false,
		}, {
			name: "Duplicate types with the same mode",
			configs: []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
				{Group: "", Resource: "secrets", Mode: "Ignore"},
				{Group: "", Resource: "secrets", Mode: "Ignore"},
			},
			validator: f,
			allow:     false,
		}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			c := &api.HNCConfiguration{Spec: api.HNCConfigurationSpec{Types: tc.configs}}
			c.Name = api.HNCConfigSingleton
			config := &HNCConfig{translator: tc.validator, Forest: forest.NewForest()}

			got := config.handle(context.Background(), c)

			logResult(t, got.AdmissionResponse.Result)
			g.Expect(got.AdmissionResponse.Allowed).Should(Equal(tc.allow))
		})
	}
}

func TestPropagateConflict(t *testing.T) {
	tests := []struct {
		name         string
		inNamespaces string
		allow        bool
	}{{
		name:         "Objects with the same name existing in namespaces that one is not an ancestor of the other would not cause overwriting conflict",
		inNamespaces: "bc",
		allow:        true,
	}, {
		name:         "Objects with the same name existing in namespaces that one is an ancestor of the other would have overwriting conflict",
		inNamespaces: "ab",
		allow:        false,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			configs := []api.TypeSynchronizationSpec{
				{Group: api.RBACGroup, Resource: api.RoleResource, Mode: "Propagate"},
				{Group: api.RBACGroup, Resource: api.RoleBindingResource, Mode: "Propagate"},
				{Group: "", Resource: "secrets", Mode: "Propagate"}}
			c := &api.HNCConfiguration{Spec: api.HNCConfigurationSpec{Types: configs}}
			c.Name = api.HNCConfigSingleton
			// Create a forest with "a" as the parent and "b" and "c" as the children.
			f := foresttest.Create("-aa")
			config := &HNCConfig{translator: fakeGRTranslator{}, Forest: f}

			// Add source objects to the forest.
			inst := &unstructured.Unstructured{}
			inst.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"})
			inst.SetName("my-creds")
			for _, c := range tc.inNamespaces {
				f.Get(string(c)).SetSourceObject(inst)
			}
			got := config.handle(context.Background(), c)

			logResult(t, got.AdmissionResponse.Result)
			g.Expect(got.AdmissionResponse.Allowed).Should(Equal(tc.allow))
		})
	}
}

// fakeGRTranslator implements grTranslator. Any kind that are in the slice are
// denied; anything else are translated.
type fakeGRTranslator []string

func (f fakeGRTranslator) GVKFor(gr schema.GroupResource) (schema.GroupVersionKind, error) {
	for _, r := range f {
		if r == gr.Resource {
			return schema.GroupVersionKind{}, fmt.Errorf("%s does not exist", gr)
		}
	}
	return gr2gvk[gr], nil
}
