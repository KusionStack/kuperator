/*
Copyright 2025 The KusionStack Authors.

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

package xcollaset

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/kube-xset/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_xSetControllerLabelManager_Get(t *testing.T) {
	type fields struct {
		labelManager map[api.XSetLabelAnnotationEnum]string
	}
	type args struct {
		pod *corev1.Pod
		key api.XSetLabelAnnotationEnum
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
		want1  bool
	}{
		{
			name: "get label ok",
			fields: fields{
				labelManager: defaultXSetControllerLabelManager,
			},
			args: args{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							appsv1alpha1.PodInstanceIDLabelKey: "0",
						},
					},
				},
				key: api.XInstanceIdLabelKey,
			},
			want:  "0",
			want1: true,
		},
		{
			name: "label is nil",
			fields: fields{
				labelManager: defaultXSetControllerLabelManager,
			},
			args: args{
				pod: &corev1.Pod{},
				key: api.XInstanceIdLabelKey,
			},
			want:  "",
			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := api.NewXSetLabelAnnotationManager(defaultXSetControllerLabelManager)
			got, got1 := m.Get(tt.args.pod, tt.args.key)
			if got != tt.want {
				t.Errorf("Get() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("Get() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_xSetControllerLabelManager_Set(t *testing.T) {
	type fields struct {
		labelManager map[api.XSetLabelAnnotationEnum]string
	}
	type args struct {
		obj client.Object
		key api.XSetLabelAnnotationEnum
		val string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "object label is nil",
			fields: fields{
				labelManager: defaultXSetControllerLabelManager,
			},
			args: args{
				obj: &corev1.Pod{},
				key: api.XInstanceIdLabelKey,
				val: "0",
			},
		},
		{
			name: "set label ok",
			fields: fields{
				labelManager: defaultXSetControllerLabelManager,
			},
			args: args{
				obj: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							appsv1alpha1.PodInstanceIDLabelKey: "0",
						},
					},
				},
				key: api.XInstanceIdLabelKey,
				val: "0",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := api.NewXSetLabelAnnotationManager(defaultXSetControllerLabelManager)
			m.Set(tt.args.obj, tt.args.key, tt.args.val)
			if val, ok := m.Get(tt.args.obj, tt.args.key); !ok || val != tt.args.val {
				t.Errorf("Set() = %v, want %v", val, tt.args.val)
			}
		})
	}
}

func Test_xSetControllerLabelManager_Delete(t *testing.T) {
	type fields struct {
		labelManager map[api.XSetLabelAnnotationEnum]string
	}
	type args struct {
		pod *corev1.Pod
		key api.XSetLabelAnnotationEnum
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "delete label ok",
			fields: fields{
				labelManager: defaultXSetControllerLabelManager,
			},
			args: args{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							appsv1alpha1.PodInstanceIDLabelKey: "0",
						},
					},
				},
				key: api.XInstanceIdLabelKey,
			},
		},
		{
			name: "delete label is nil",
			fields: fields{
				labelManager: defaultXSetControllerLabelManager,
			},
			args: args{
				pod: &corev1.Pod{},
				key: api.XInstanceIdLabelKey,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := api.NewXSetLabelAnnotationManager(defaultXSetControllerLabelManager)
			m.Delete(tt.args.pod, tt.args.key)
			if val, ok := m.Get(tt.args.pod, tt.args.key); ok || val != "" {
				t.Errorf("Delete() = %v, want %v", val, "")
			}
		})
	}
}
