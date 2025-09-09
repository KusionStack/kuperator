package utils

import (
	"testing"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func TestPodNamingSuffixPersistentSequence(t *testing.T) {
	type args struct {
		cls *appsv1alpha1.CollaSet
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "nil naming strategy",
			args: args{
				cls: &appsv1alpha1.CollaSet{
					Spec: appsv1alpha1.CollaSetSpec{},
				},
			},
			want: false,
		},
		{
			name: "persistent sequence naming suffix",
			args: args{
				cls: &appsv1alpha1.CollaSet{
					Spec: appsv1alpha1.CollaSetSpec{
						NamingStrategy: &appsv1alpha1.NamingStrategy{
							PodNamingSuffixPolicy: appsv1alpha1.PodNamingSuffixPolicyPersistentSequence,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "random naming suffix",
			args: args{
				cls: &appsv1alpha1.CollaSet{
					Spec: appsv1alpha1.CollaSetSpec{
						NamingStrategy: &appsv1alpha1.NamingStrategy{
							PodNamingSuffixPolicy: appsv1alpha1.PodNamingSuffixPolicyRandom,
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodNamingSuffixPersistentSequence(tt.args.cls); got != tt.want {
				t.Errorf("PodNamingSuffixPersistentSequence() = %v, want %v", got, tt.want)
			}
		})
	}
}
