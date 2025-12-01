// Copyright 2019 Huawei Technologies Co.,Ltd.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use
// this file except in compliance with the License.  You may obtain a copy of the
// License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed
// under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations under the License.

package obs

import (
	"encoding/json"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	accessKeyEnv     = "OBS_ACCESS_KEY_ID"
	securityKeyEnv   = "OBS_SECRET_ACCESS_KEY"
	securityTokenEnv = "OBS_SECURITY_TOKEN"
	ecsRequestURL    = "http://169.254.169.254/openstack/latest/securitykey"
)

type securityHolder struct {
	ak            string
	sk            string
	securityToken string
}

var emptySecurityHolder = securityHolder{}

type securityProvider interface {
	getSecurity() securityHolder
}

type BasicSecurityProvider struct {
	val atomic.Value
}

func (bsp *BasicSecurityProvider) getSecurity() securityHolder {
	if sh, ok := bsp.val.Load().(securityHolder); ok {
		return sh
	}
	return emptySecurityHolder
}

func (bsp *BasicSecurityProvider) refresh(ak, sk, securityToken string) {
	bsp.val.Store(securityHolder{ak: strings.TrimSpace(ak), sk: strings.TrimSpace(sk), securityToken: strings.TrimSpace(securityToken)})
}

func NewBasicSecurityProvider(ak, sk, securityToken string) *BasicSecurityProvider {
	bsp := &BasicSecurityProvider{}
	bsp.refresh(ak, sk, securityToken)
	return bsp
}

type EnvSecurityProvider struct {
	sh     securityHolder
	suffix string
	once   sync.Once
}

func (esp *EnvSecurityProvider) getSecurity() securityHolder {
	//ensure run only once
	esp.once.Do(func() {
		esp.sh = securityHolder{
			ak:            strings.TrimSpace(os.Getenv(accessKeyEnv + esp.suffix)),
			sk:            strings.TrimSpace(os.Getenv(securityKeyEnv + esp.suffix)),
			securityToken: strings.TrimSpace(os.Getenv(securityTokenEnv + esp.suffix)),
		}
	})

	return esp.sh
}

func NewEnvSecurityProvider(suffix string) *EnvSecurityProvider {
	if suffix != "" {
		suffix = "_" + suffix
	}
	esp := &EnvSecurityProvider{
		suffix: suffix,
	}
	return esp
}

type TemporarySecurityHolder struct {
	securityHolder
	expireDate time.Time
}

var emptyTemporarySecurityHolder = TemporarySecurityHolder{}

type EcsSecurityProvider struct {
	val        atomic.Value
	lock       sync.Mutex
	httpClient *http.Client
	prefetch   int32
	retryCount int
}

func (ecsSp *EcsSecurityProvider) loadTemporarySecurityHolder() (TemporarySecurityHolder, bool) {
	if sh := ecsSp.val.Load(); sh == nil {
		return emptyTemporarySecurityHolder, false
	} else if _sh, ok := sh.(TemporarySecurityHolder); !ok {
		return emptyTemporarySecurityHolder, false
	} else {
		return _sh, true
	}
}

func (ecsSp *EcsSecurityProvider) getAndSetSecurityWithOutLock() securityHolder {
	_sh := TemporarySecurityHolder{}
	_sh.expireDate = time.Now().Add(time.Minute * 5)
	retryCount := 0
	for {
		if req, err := http.NewRequest("GET", ecsRequestURL, nil); err == nil {
			start := GetCurrentTimestamp()
			res, err := ecsSp.httpClient.Do(req)
			if err == nil {
				if data, _err := ioutil.ReadAll(res.Body); _err == nil {
					temp := &struct {
						Credential struct {
							AK            string    `json:"access,omitempty"`
							SK            string    `json:"secret,omitempty"`
							SecurityToken string    `json:"securitytoken,omitempty"`
							ExpireDate    time.Time `json:"expires_at,omitempty"`
						} `json:"credential"`
					}{}

					doLog(LEVEL_DEBUG, "Get the json data from ecs succeed")

					if jsonErr := json.Unmarshal(data, temp); jsonErr == nil {
						_sh.ak = temp.Credential.AK
						_sh.sk = temp.Credential.SK
						_sh.securityToken = temp.Credential.SecurityToken
						_sh.expireDate = temp.Credential.ExpireDate.Add(time.Minute * -1)

						doLog(LEVEL_INFO, "Get security from ecs succeed, AK:xxxx, SK:xxxx, SecurityToken:xxxx, ExprireDate %s", _sh.expireDate)

						doLog(LEVEL_INFO, "Get security from ecs succeed, cost %d ms", (GetCurrentTimestamp() - start))
						break
					} else {
						err = jsonErr
					}
				} else {
					err = _err
				}
			}

			doLog(LEVEL_WARN, "Try to get security from ecs failed, cost %d ms, err %s", (GetCurrentTimestamp() - start), err.Error())
		}

		if retryCount >= ecsSp.retryCount {
			doLog(LEVEL_WARN, "Try to get security from ecs failed and exceed the max retry count")
			break
		}
		sleepTime := float64(retryCount+2) * rand.Float64()
		if sleepTime > 10 {
			sleepTime = 10
		}
		time.Sleep(time.Duration(sleepTime * float64(time.Second)))
		retryCount++
	}

	ecsSp.val.Store(_sh)
	return _sh.securityHolder
}

func (ecsSp *EcsSecurityProvider) getAndSetSecurity() securityHolder {
	ecsSp.lock.Lock()
	defer ecsSp.lock.Unlock()
	tsh, succeed := ecsSp.loadTemporarySecurityHolder()
	if !succeed || time.Now().After(tsh.expireDate) {
		return ecsSp.getAndSetSecurityWithOutLock()
	}
	return tsh.securityHolder
}

func (ecsSp *EcsSecurityProvider) getSecurity() securityHolder {
	if tsh, succeed := ecsSp.loadTemporarySecurityHolder(); succeed {
		if time.Now().Before(tsh.expireDate) {
			//not expire
			if time.Now().Add(time.Minute*5).After(tsh.expireDate) && atomic.CompareAndSwapInt32(&ecsSp.prefetch, 0, 1) {
				//do prefetch
				sh := ecsSp.getAndSetSecurityWithOutLock()
				atomic.CompareAndSwapInt32(&ecsSp.prefetch, 1, 0)
				return sh
			}
			return tsh.securityHolder
		}
		return ecsSp.getAndSetSecurity()
	}

	return ecsSp.getAndSetSecurity()
}

func getInternalTransport() *http.Transport {
	timeout := 10
	transport := &http.Transport{
		Dial: func(network, addr string) (net.Conn, error) {
			start := GetCurrentTimestamp()
			conn, err := (&net.Dialer{
				Timeout:  time.Second * time.Duration(timeout),
				Resolver: net.DefaultResolver,
			}).Dial(network, addr)

			if isInfoLogEnabled() {
				doLog(LEVEL_INFO, "Do http dial cost %d ms", (GetCurrentTimestamp() - start))
			}
			if err != nil {
				return nil, err
			}
			return getConnDelegate(conn, timeout, timeout*10), nil
		},
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   10,
		ResponseHeaderTimeout: time.Second * time.Duration(timeout),
		IdleConnTimeout:       time.Second * time.Duration(DEFAULT_IDLE_CONN_TIMEOUT),
		DisableCompression:    true,
	}

	return transport
}

func NewEcsSecurityProvider(retryCount int) *EcsSecurityProvider {
	ecsSp := &EcsSecurityProvider{
		retryCount: retryCount,
	}
	ecsSp.httpClient = &http.Client{Transport: getInternalTransport(), CheckRedirect: checkRedirectFunc}
	return ecsSp
}
