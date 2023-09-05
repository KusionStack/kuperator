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

package alibabacloudslb

import (
	"fmt"
	"os"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	slb20140515 "github.com/alibabacloud-go/slb-20140515/v4/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
)

var (
	slbAccessKeyID     string
	slbAccessKeySecret string
	slbEndpoint        string
)

type AlibabaCloudSlbClient struct {
	*slb20140515.Client
}

func NewAlibabaCloudSlbClient() (*AlibabaCloudSlbClient, error) {
	if slbAccessKeyID == "" || slbAccessKeySecret == "" {
		return nil, fmt.Errorf("slbAccessKeyID or slbAccessKeySecret is empty")
	}
	config := &openapi.Config{
		AccessKeyId:     tea.String(slbAccessKeyID),
		AccessKeySecret: tea.String(slbAccessKeySecret),
		// Endpoint, refer: https://api.aliyun.com/product/Slb
		Endpoint: tea.String(slbEndpoint),
	}

	slbClient, err := slb20140515.NewClient(config)
	if err != nil {
		return nil, err
	}
	return &AlibabaCloudSlbClient{
		slbClient,
	}, nil
}

func (c *AlibabaCloudSlbClient) GetBackendServers(lbID string) ([]string, error) {
	describeHealthStatusRequest := &slb20140515.DescribeHealthStatusRequest{
		LoadBalancerId: tea.String(lbID),
	}
	runtime := &util.RuntimeOptions{}

	resp, err := c.DescribeHealthStatusWithOptions(describeHealthStatusRequest, runtime)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Body == nil || resp.Body.BackendServers == nil {
		return nil, fmt.Errorf("get backend servers list faield, resp is nil")
	}

	backendServers := make([]string, len(resp.Body.BackendServers.BackendServer))
	for idx, bs := range resp.Body.BackendServers.BackendServer {
		backendServers[idx] = *bs.ServerIp
	}
	return backendServers, nil
}

func init() {
	if os.Getenv("ALIYUN_SLB_AK_KEY") != "" {
		slbAccessKeyID = os.Getenv("ALIYUN_SLB_AK_KEY")
	}
	if os.Getenv("ALIYUN_SLB_AK_SECRET") != "" {
		slbAccessKeySecret = os.Getenv("ALIYUN_SLB_AK_SECRET")
	}
	if os.Getenv("ALIYUN_SLB_ENDPOINT") != "" {
		slbEndpoint = os.Getenv("ALIYUN_SLB_ENDPOINT")
	}
}
