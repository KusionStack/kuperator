/*
Copyright 2023 The KusionStack Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
