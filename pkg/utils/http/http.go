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

package http

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const timeout = time.Second * 30

func ParseResponse(resp *http.Response, data interface{}) error {
	if resp == nil {
		return fmt.Errorf("response cannot be nil")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	httpContentString := string(b)
	if resp.StatusCode > 199 && resp.StatusCode < 300 {
		if data != nil {
			err := json.Unmarshal(b, data)
			if err != nil {
				return fmt.Errorf("err: %s, response: %s", err, httpContentString)
			}
		}
		return nil
	}
	return fmt.Errorf("failed by response status code: %d, body: %s", resp.StatusCode, httpContentString)
}

/*
DoHttpAndHttpsRequestWithCa only used by lifecycleHook and ruleSet controller. Ca with base64
*/
func DoHttpAndHttpsRequestWithCa(method, url string, body interface{}, header map[string]string, ca string) (*http.Response, error) {
	req, err := buildReq(method, url, body, header)
	if err != nil {
		return nil, err
	}
	c, err := DefaultClient.GetClientWithCa(ca)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func DoHttpAndHttpsRequestWithToken(method, url string, body interface{}, header map[string]string, token string) (*http.Response, error) {
	req, err := buildReq(method, url, body, header)
	if err != nil {
		return nil, err
	}
	c, err := DefaultClient.GetClientWithKey(token)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func buildReq(method, url string, body interface{}, header map[string]string) (*http.Request, error) {
	buf := &bytes.Buffer{}
	if body != nil {
		err := json.NewEncoder(buf).Encode(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range header {
		req.Header.Set(key, value)
	}
	return req, nil
}

var DefaultClient = newSharedClient()

func newSharedClient() *clientSet {
	return &clientSet{
		caClientSet: map[string]*http.Client{},
		tkClientSet: map[string]*http.Client{},
	}
}

type clientSet struct {
	caClientSet map[string]*http.Client
	tkClientSet map[string]*http.Client
	mu          sync.RWMutex
}

func (s *clientSet) GetClientWithCa(ca string) (c *http.Client, err error) {
	s.mu.RLock()
	c, ok := s.caClientSet[ca]
	s.mu.RUnlock()
	if !ok {
		return s.newClient(&ca, nil)
	}
	return c, nil
}

func (s *clientSet) GetClientWithKey(key string) (c *http.Client, err error) {
	s.mu.RLock()
	c, ok := s.tkClientSet[key]
	s.mu.RUnlock()
	if !ok {
		return s.newClient(nil, &key)
	}
	return c, nil
}

/*
 *  newClient.
 *	Case 1: Different ca use different client.
 *  Case 2: Nil ca use default client which ClientCAs is systemPool
 *  Case 3: Different token use different client and ClientCAs is systemPool
 */
func (s *clientSet) newClient(ca *string, key *string) (c *http.Client, err error) {
	var pool *x509.CertPool
	if ca != nil && *ca != "" && *ca != "Cg==" {
		pool = x509.NewCertPool()
		bt, err := base64.StdEncoding.DecodeString(*ca)
		if err != nil {
			return nil, err
		}
		pool.AppendCertsFromPEM(bt)
	}
	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			ClientCAs: pool,
		},
	}
	c = &http.Client{Transport: t, Timeout: timeout}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ca != nil {
		s.caClientSet[*ca] = c
	} else if key != nil {
		s.tkClientSet[*key] = c
	}
	return c, err
}
