package utils

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func TestAllowResourceExclude(t *testing.T) {
	var ownerName = "test"
	var ownerKind = "CollaSet"
	tests := []struct {
		name   string
		obj    *corev1.Pod
		allow  bool
		reason string
	}{
		{
			name: "label is nil",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: nil,
				},
			},
			allow:  false,
			reason: "object's label is empty",
		},
		{
			name: "KusionStack control label not satisfied",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "false",
					},
				},
			},
			allow:  false,
			reason: "object is not controlled by kusionstack system",
		},
		{
			name: "controller is nil",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
					OwnerReferences: make([]metav1.OwnerReference, 0),
				},
			},
			allow:  false,
			reason: "object is not owned by any one, not allowed to exclude",
		},
		{
			name: "controller name not equals to ownerName",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       "test1",
							Kind:       ownerKind,
							Controller: pointer.Bool(true),
						},
					},
				},
			},
			allow:  false,
			reason: "object is not owned by any one, not allowed to exclude",
		},
		{
			name: "controller kind not equals to ownerName",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       ownerName,
							Kind:       "kind2",
							Controller: pointer.Bool(true),
						},
					},
				},
			},
			allow:  false,
			reason: "object is not owned by any one, not allowed to exclude",
		},
		{
			name: "allowed case",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       ownerName,
							Kind:       ownerKind,
							Controller: pointer.Bool(true),
						},
					},
				},
			},
			allow:  true,
			reason: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := AllowResourceExclude(tt.obj, ownerName, ownerKind)
			if got != tt.allow {
				t.Errorf("AllowResourceExclude() got = %v, want %v", got, tt.allow)
			}
			if got1 != tt.reason {
				t.Errorf("AllowResourceExclude() got1 = %v, want %v", got1, tt.reason)
			}
		})
	}
}

func TestAllowResourceInclude(t *testing.T) {
	var ownerName = "test"
	var ownerKind = "CollaSet"
	tests := []struct {
		name   string
		obj    *corev1.Pod
		allow  bool
		reason string
	}{
		{
			name: "label is nil",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: nil,
				},
			},
			allow:  false,
			reason: "object's label is empty",
		},
		{
			name: "KusionStack control label not satisfied",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "false",
					},
				},
			},
			allow:  false,
			reason: "object is not controlled by kusionstack system",
		},
		{
			name: "controller is nil",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
					OwnerReferences: make([]metav1.OwnerReference, 0),
				},
			},
			allow:  true,
			reason: "",
		},
		{
			name: "controller name not equals to ownerName",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       "test1",
							Kind:       ownerKind,
							Controller: pointer.Bool(true),
						},
					},
				},
			},
			allow:  false,
			reason: "object's ownerReference controller is not CollaSet/test",
		},
		{
			name: "controller kind not equals to ownerName",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       "test",
							Kind:       "kind2",
							Controller: pointer.Bool(true),
						},
					},
				},
			},
			allow:  false,
			reason: "object's ownerReference controller is not CollaSet/test",
		},
		{
			name: "controller kind not equals to ownerName",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       ownerName,
							Kind:       ownerKind,
							Controller: pointer.Bool(true),
						},
					},
				},
			},
			allow:  false,
			reason: "object's is controlled by CollaSet/test, but marked as orphaned",
		},
		{
			name: "allowed case1",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
						appsv1alpha1.PodOrphanedIndicateLabelKey:     "true",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       ownerName,
							Kind:       ownerKind,
							Controller: pointer.Bool(true),
						},
					},
				},
			},
			allow:  true,
			reason: "",
		},
		{
			name: "allowed case2",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
					},
				},
			},
			allow:  true,
			reason: "",
		},
		{
			name: "allowed case3",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1alpha1.ControlledByKusionStackLabelKey: "true",
						appsv1alpha1.PodOrphanedIndicateLabelKey:     "true",
					},
				},
			},
			allow:  true,
			reason: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := AllowResourceInclude(tt.obj, ownerName, ownerKind)
			if got != tt.allow {
				t.Errorf("AllowResourceExclude() got = %v, want %v", got, tt.allow)
			}
			if got1 != tt.reason {
				t.Errorf("AllowResourceExclude() got1 = %v, want %v", got1, tt.reason)
			}
		})
	}
}
