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

package podtransitionrule

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"kusionstack.io/operating/pkg/webhook/server/generic"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	commonutils "kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
)

func init() {
	generic.ValidatingTypeHandlerMap["PodTransitionRule"] = NewValidatingHandler()
}

var _ inject.Client = &ValidatingHandler{}
var _ admission.DecoderInjector = &ValidatingHandler{}

type ValidatingHandler struct {
	*mixin.WebhookHandlerMixin
}

func NewValidatingHandler() *ValidatingHandler {
	return &ValidatingHandler{
		WebhookHandlerMixin: mixin.NewWebhookHandlerMixin(),
	}
}

func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	if req.Kind.Kind != "PodTransitionRule" || req.Operation == admissionv1.Delete {
		return admission.Allowed("")
	}

	logger := h.Logger.WithValues(
		"op", req.Operation,
		"podtransitionrule", commonutils.AdmissionRequestObjectKeyString(req),
	)

	rs := &appsv1alpha1.PodTransitionRule{}

	if err := h.Decoder.Decode(req, rs); err != nil {
		logger.Error(err, "failed to decode podtransitionrule")
		return admission.Errored(http.StatusBadRequest, err)
	}
	if err := h.validate(rs); err != nil {
		logger.Error(err, "illegal PodTransitionRule")
		return admission.Denied(err.Error())
	}
	return admission.Allowed("")
}

func (h *ValidatingHandler) validate(rs *appsv1alpha1.PodTransitionRule) error {
	var errList field.ErrorList
	fSpec := field.NewPath("spec")

	if rs.Spec.Selector == nil {
		return fmt.Errorf("podtransitionrule selector cannot be nil")
	}
	fRule := fSpec.Child("rule")
	for _, rule := range rs.Spec.Rules {
		if rule.Name == "" {
			return fmt.Errorf("podtransitionrule rule name is required")
		}
		if rule.Webhook != nil {
			if err := ValidateWebhook(rule.Webhook, fRule.Child(rule.Name)); err != nil {
				errList = append(errList, err)
			}
		}
		if rule.LabelCheck != nil && rule.LabelCheck.Requires == nil {
			errList = append(errList, field.Invalid(fRule.Child(rule.Name), nil, "nil label check required"))
		}
		if rule.AvailablePolicy != nil && rule.AvailablePolicy.MaxUnavailableValue == nil && rule.AvailablePolicy.MinAvailableValue == nil {
			errList = append(errList, field.Invalid(fRule.Child(rule.Name), nil, "minAvailableValue and maxUnavailableValue must have at least one configured"))
		}
	}
	return errList.ToAggregate()
}

func ValidateWebhook(webhook *appsv1alpha1.TransitionRuleWebhook, f *field.Path) *field.Error {

	if err := CheckServerReachable(webhook.ClientConfig.URL); err != nil {
		return field.Invalid(f.Child("clientConfig").Child("url"), webhook.ClientConfig.URL, err.Error())
	}
	if err := CheckCaBundle(webhook.ClientConfig.CABundle); err != nil {
		return field.Invalid(f.Child("clientConfig").Child("caBundle"), webhook.ClientConfig.CABundle, err.Error())
	}
	return nil
}

func CheckServerReachable(serverUrl string) error {
	u, err := url.Parse(serverUrl)
	if err != nil {
		return fmt.Errorf("podtransitionrule validate webhook failed, check server reachable, while parse url error: %s", err)
	}

	if !strings.Contains(u.Host, ":") {
		if u.Scheme == "http" {
			u.Host = u.Host + ":80"
		} else {
			u.Host = u.Host + ":443"
		}
	}

	timeout := time.Duration(5) * time.Second
	_, err = net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		return fmt.Errorf("podtransitionrule validate webhook failed, check server reachable, while server unreachable, error: %s", err)
	}

	return nil
}

func CheckCaBundle(ca string) error {
	if ca == "" || ca == "Cg==" {
		return nil
	}
	caByte, err := base64.StdEncoding.DecodeString(ca)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(caByte)
	if block == nil {
		return fmt.Errorf("failed to parse CABundle PEM, %s", string(caByte))
	}
	_, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CABundle certificate, %v", err)
	}
	return nil
}
