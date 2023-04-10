package rules_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/handlers/internal/rules"
)

func TestList_AlertingRulesMatchSelector_WithDefaultStackNamespaceRules(t *testing.T) {
	const stackNs = "some-ns"

	k := &k8sfakes.FakeClient{}
	rs := &lokiv1.RulesSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"labelname": "labelvalue",
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == stackNs {
			k.SetClientObject(object, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: stackNs,
				},
			})
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.ListStub = func(_ context.Context, ol client.ObjectList, opt ...client.ListOption) error {
		switch ol.(type) {
		case *corev1.NamespaceList:
			k.SetClientObjectList(ol, &corev1.NamespaceList{})
			return nil
		case *lokiv1.RecordingRuleList:
			k.SetClientObjectList(ol, &lokiv1.RecordingRuleList{})
			return nil
		}

		l := opt[0].(*client.MatchingLabelsSelector)
		m := labels.Set(rs.Selector.MatchLabels)

		if l.Matches(m) {
			k.SetClientObjectList(ol, &lokiv1.AlertingRuleList{
				Items: []lokiv1.AlertingRule{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-a",
							Namespace: stackNs,
							Labels: map[string]string{
								"labelname": "labelvalue",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-b",
							Namespace: "other-ns",
							Labels: map[string]string{
								"labelname": "labelvalue",
							},
						},
					},
				},
			})
		}

		return nil
	}

	rules, _, err := rules.List(context.TODO(), k, stackNs, rs)

	require.NoError(t, err)
	require.NotEmpty(t, rules)
	require.Len(t, rules, 1)
}

func TestList_AlertingRulesMatchSelector_FilteredByNamespaceSelector(t *testing.T) {
	const stackNs = "some-ns"

	k := &k8sfakes.FakeClient{}
	rs := &lokiv1.RulesSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"labelname": "labelvalue",
			},
		},
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"group.acme.org/logs": "true",
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == "some-ns" {
			k.SetClientObject(object, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: stackNs,
				},
			})
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.ListStub = func(_ context.Context, ol client.ObjectList, opt ...client.ListOption) error {
		switch ol.(type) {
		case *lokiv1.RecordingRuleList:
			k.SetClientObjectList(ol, &lokiv1.RecordingRuleList{})
			return nil
		}

		l := opt[0].(*client.MatchingLabelsSelector)
		m := labels.Set(rs.Selector.MatchLabels)

		if l.Matches(m) {
			k.SetClientObjectList(ol, &lokiv1.AlertingRuleList{
				Items: []lokiv1.AlertingRule{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-a",
							Namespace: "matching-ns",
							Labels: map[string]string{
								"labelname": "labelvalue",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-b",
							Namespace: stackNs,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-c",
							Namespace: "not-matching-ns",
							Labels: map[string]string{
								"labelname": "labelvalue",
							},
						},
					},
				},
			})

			return nil
		}

		n := labels.Set(rs.NamespaceSelector.MatchLabels)
		if l.Matches(n) {
			k.SetClientObjectList(ol, &corev1.NamespaceList{
				Items: []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "matching-ns",
							Labels: map[string]string{
								"group.acme.org/logs": "true",
							},
						},
					},
				},
			})

			return nil
		}

		k.SetClientObjectList(ol, &corev1.NamespaceList{})

		return nil
	}

	rules, _, err := rules.List(context.TODO(), k, stackNs, rs)

	require.NoError(t, err)
	require.NotEmpty(t, rules)
	require.Len(t, rules, 2)
}

func TestList_RecordingRulesMatchSelector_WithDefaultStackNamespaceRules(t *testing.T) {
	const stackNs = "some-ns"

	k := &k8sfakes.FakeClient{}
	rs := &lokiv1.RulesSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"labelname": "labelvalue",
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == stackNs {
			k.SetClientObject(object, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: stackNs,
				},
			})
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.ListStub = func(_ context.Context, ol client.ObjectList, opt ...client.ListOption) error {
		switch ol.(type) {
		case *corev1.NamespaceList:
			k.SetClientObjectList(ol, &corev1.NamespaceList{})
			return nil
		case *lokiv1.AlertingRuleList:
			k.SetClientObjectList(ol, &lokiv1.AlertingRuleList{})
			return nil
		}

		l := opt[0].(*client.MatchingLabelsSelector)
		m := labels.Set(rs.Selector.MatchLabels)

		if l.Matches(m) {
			k.SetClientObjectList(ol, &lokiv1.RecordingRuleList{
				Items: []lokiv1.RecordingRule{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-a",
							Namespace: stackNs,
							Labels: map[string]string{
								"labelname": "labelvalue",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-b",
							Namespace: "other-ns",
							Labels: map[string]string{
								"labelname": "labelvalue",
							},
						},
					},
				},
			})
		}

		return nil
	}

	_, rules, err := rules.List(context.TODO(), k, stackNs, rs)

	require.NoError(t, err)
	require.NotEmpty(t, rules)
	require.Len(t, rules, 1)
}

func TestList_RecordingRulesMatchSelector_FilteredByNamespaceSelector(t *testing.T) {
	const stackNs = "some-ns"

	k := &k8sfakes.FakeClient{}
	rs := &lokiv1.RulesSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"labelname": "labelvalue",
			},
		},
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"group.acme.org/logs": "true",
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == "some-ns" {
			k.SetClientObject(object, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: stackNs,
				},
			})
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.ListStub = func(_ context.Context, ol client.ObjectList, opt ...client.ListOption) error {
		switch ol.(type) {
		case *lokiv1.AlertingRuleList:
			k.SetClientObjectList(ol, &lokiv1.AlertingRuleList{})
			return nil
		}

		l := opt[0].(*client.MatchingLabelsSelector)
		m := labels.Set(rs.Selector.MatchLabels)

		if l.Matches(m) {
			k.SetClientObjectList(ol, &lokiv1.RecordingRuleList{
				Items: []lokiv1.RecordingRule{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-a",
							Namespace: "matching-ns",
							Labels: map[string]string{
								"labelname": "labelvalue",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-b",
							Namespace: stackNs,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-c",
							Namespace: "not-matching-ns",
							Labels: map[string]string{
								"labelname": "labelvalue",
							},
						},
					},
				},
			})

			return nil
		}

		n := labels.Set(rs.NamespaceSelector.MatchLabels)
		if l.Matches(n) {
			k.SetClientObjectList(ol, &corev1.NamespaceList{
				Items: []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "matching-ns",
							Labels: map[string]string{
								"group.acme.org/logs": "true",
							},
						},
					},
				},
			})
			return nil
		}

		k.SetClientObjectList(ol, &corev1.NamespaceList{})

		return nil
	}

	_, rules, err := rules.List(context.TODO(), k, stackNs, rs)

	require.NoError(t, err)
	require.NotEmpty(t, rules)
	require.Len(t, rules, 2)
}
