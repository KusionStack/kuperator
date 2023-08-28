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

package ruleset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
)

func TestValidate(t *testing.T) {
	testCA := "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUMvVENDQWVXZ0F3SUJBZ0lCQURBTkJna3Foa2lHOXcwQkFRc0ZBREFmTVIwd0d3WURWUVFERXhSelpXeG0KTFhOcFoyNWxaQzFyT0hNdFkyVnlkREFnRncweU16QTNNVGN3TmpJNU16RmFHQTh5TVRJek1EWXlNekEyTWpregpNVm93SHpFZE1Cc0dBMVVFQXhNVWMyVnNaaTF6YVdkdVpXUXRhemh6TFdObGNuUXdnZ0VpTUEwR0NTcUdTSWIzCkRRRUJBUVVBQTRJQkR3QXdnZ0VLQW9JQkFRRHhEQytBS05oSGVxRThqaThBNllIUGNZbGVyeERrMmNoQlpabzAKaEJqTUpMZno1STU0aENhR0wwT08vMU0yMFQyZnFZWEFrRWwzRnlhU1VIY3liNnNGbEMwWHRkLzVaK0tMZkRKTgpTK2YrdHB3QmxZZ3U0S2hHN1U5VmpiV3RZRWk2OGZKNFNIRHBGd3BWZnFzSzhhVjYrZis1cElPclZFYS9rbmhsCitFd2ZBeG1uNm1xVlpZQXhManBVNXF3TERqU3ZXcnhIcTQ2UWx1eTBwV09maXBYelg4L3BLT0d2YWN6L1R2emMKRy9uNnY1NDNSeXArV05PV0hvajdSTXA3YTVYczdQcjFMM040ZjhscWJkMWs3WGZJa1lXWlR2OWpqeFRFRFp6Wgo3Y3BwRXZ1OHRBK0MxVzhMeDdOSGk1a1BXcjM5YUhkb201NUpHT2tWZDdEdDB2RHhBZ01CQUFHalFqQkFNQTRHCkExVWREd0VCL3dRRUF3SUNwREFQQmdOVkhSTUJBZjhFQlRBREFRSC9NQjBHQTFVZERnUVdCQlN3c05CZjRjbVoKMFV4S0pwWCtpblUvWGdKWHdUQU5CZ2txaGtpRzl3MEJBUXNGQUFPQ0FRRUFXWFNibXlMWXAwZHFXTjVaaHNXVgphWUwxbEh4SmlyaE5IbHZqYkM2cXpnd2VUNWRJWFB6U3lQZ25DajBDOHJ1bHJiQUV4R3Jva1hkQzJiVTBoYUw3CngxU2M4R1lPSU9pSFdHQnM1VitrbUh0bzdmeVR4cFV0OGFSNU1TWitCZkFBNHJRZzJWRWNxUkkzRE9aTDdRYXAKRVZLWnpqSTJObkRRbUN2N2oxZERrajVRMWRsTW96QWRlN1ZUZXE0Y1pVTW8ydUNmeEViZlZMSXVzRXI3cmc1cwpIa2M4U3piVUpudTdDc0dRbE1JNTBMV3FxWHlkT3ZCNk5nTjhvNDNBdlY0ck9NOGx5WklnbG14ZkRPMVRyUlVCCmMza0wzZ0JHazlHZnJ0OFI1d1dOamlMQkJRQnBRMFdqVlh2QlNaSzdOcHlYSEFranVUSjlTa0ZaQXBUZ2JmM2UKSFE9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg=="
	invalidCA := "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUMvVENDQWVXZ0F3SUJBZ0lCQURBTkJna3Foa"
	webhook := &appsv1alpha1.RuleSetRuleWebhook{
		ClientConfig: appsv1alpha1.ClientConfig{
			URL:      "https://github.com",
			CABundle: "Cg==",
		},
	}
	assert.Nil(t, ValidateWebhook(webhook, field.NewPath("test")))
	webhook.ClientConfig.URL = "https://xxx.baidu.xkdsa"
	assert.Error(t, ValidateWebhook(webhook, field.NewPath("test")))
	assert.Nil(t, CheckCaBundle(testCA))
	assert.Nil(t, CheckCaBundle("Cg=="))
	assert.Error(t, CheckCaBundle(invalidCA))

	rs := &appsv1alpha1.RuleSet{
		Spec: appsv1alpha1.RuleSetSpec{},
	}
	assert.Error(t, NewValidatingHandler().validate(rs))
	rs.Spec = appsv1alpha1.RuleSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"test": "test"},
		},
		Rules: []appsv1alpha1.RuleSetRule{
			{
				Name:                  "",
				RuleSetRuleDefinition: appsv1alpha1.RuleSetRuleDefinition{},
			},
		},
	}
	assert.Error(t, NewValidatingHandler().validate(rs))
	rs.Spec = appsv1alpha1.RuleSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"test": "test"},
		},
		Rules: []appsv1alpha1.RuleSetRule{
			{
				Name: "webhook",
				RuleSetRuleDefinition: appsv1alpha1.RuleSetRuleDefinition{
					Webhook: &appsv1alpha1.RuleSetRuleWebhook{},
				},
			},
		},
	}
	assert.Error(t, NewValidatingHandler().validate(rs))
	rs.Spec = appsv1alpha1.RuleSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"test": "test"},
		},
		Rules: []appsv1alpha1.RuleSetRule{
			{
				Name: "webhook",
				RuleSetRuleDefinition: appsv1alpha1.RuleSetRuleDefinition{
					Webhook: &appsv1alpha1.RuleSetRuleWebhook{
						ClientConfig: appsv1alpha1.ClientConfig{
							URL: "https://github.com",
						},
					},
				},
			},
		},
	}
	assert.Nil(t, NewValidatingHandler().validate(rs))
	rs.Spec = appsv1alpha1.RuleSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"test": "test"},
		},
		Rules: []appsv1alpha1.RuleSetRule{
			{
				Name: "available",
				RuleSetRuleDefinition: appsv1alpha1.RuleSetRuleDefinition{
					AvailablePolicy: &appsv1alpha1.AvailableRule{},
				},
			},
		},
	}
	assert.Error(t, NewValidatingHandler().validate(rs))
	istr := intstr.FromString("50%")
	rs.Spec = appsv1alpha1.RuleSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"test": "test"},
		},
		Rules: []appsv1alpha1.RuleSetRule{
			{
				Name: "available",
				RuleSetRuleDefinition: appsv1alpha1.RuleSetRuleDefinition{
					AvailablePolicy: &appsv1alpha1.AvailableRule{
						MaxUnavailableValue: &istr,
					},
				},
			},
		},
	}
	assert.Nil(t, NewValidatingHandler().validate(rs))
	rs.Spec = appsv1alpha1.RuleSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"test": "test"},
		},
		Rules: []appsv1alpha1.RuleSetRule{
			{
				Name: "label",
				RuleSetRuleDefinition: appsv1alpha1.RuleSetRuleDefinition{
					LabelCheck: &appsv1alpha1.LabelCheckRule{},
				},
			},
		},
	}
	assert.Error(t, NewValidatingHandler().validate(rs))
	rs.Spec = appsv1alpha1.RuleSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"test": "test"},
		},
		Rules: []appsv1alpha1.RuleSetRule{
			{
				Name: "label",
				RuleSetRuleDefinition: appsv1alpha1.RuleSetRuleDefinition{
					LabelCheck: &appsv1alpha1.LabelCheckRule{
						Requires: &metav1.LabelSelector{
							MatchLabels: map[string]string{"test": "test"},
						},
					},
				},
			},
		},
	}
	assert.Nil(t, NewValidatingHandler().validate(rs))
}
