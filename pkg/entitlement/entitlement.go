/*
(c) 2021 Morgan Stanley

THIS SOFTWARE IS CONTRIBUTED SUBJECT TO THE TERMS OF THE Grafana Labs Software
Grant and Contributor License Agreement, V 2021-04-20 GNU.

THIS SOFTWARE IS LICENSED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE AND ANY
WARRANTY OF NON-INFRINGEMENT, ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT
OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR
BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER
IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
POSSIBILITY OF SUCH DAMAGE. THIS SOFTWARE MAY BE REDISTRIBUTED TO OTHERS ONLY
BY EFFECTIVELY USING THIS OR ANOTHER EQUIVALENT DISCLAIMER IN ADDITION TO ANY
OTHER REQUIRED LICENSE TERMS.
*/

package entitlement

import (
	context "context"
	"flag"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/weaveworks/common/user"
	grpc "google.golang.org/grpc"
)

// Entitlement  is a service to check entitlement
type Entitlement struct {
	entClient EntitlementClient
	entCache  sync.Map
	reLabels  sync.Map
	sync.RWMutex
	authzEnabled bool
}

// Config is a data structure for the Entitlement config
type Config struct {
	GrpcServer        string              `yaml:"grpc_server"`
	LabelKey          string              `yaml:"label_key"`
	DefaultAllow      bool                `yaml:"allow_access_if_label_key_doesnt_exist"`
	TrustedCnames     flagext.StringSlice `yaml:"trusted_cnames"`
	UserIDHeader      string              `yaml:"userid_header"`
	CacheRetainPeriod time.Duration       `yaml:"cache_retain_period"`
}

type entitlementResult struct {
	timestamp int64
	entitled  bool
}

var ent *Entitlement = &Entitlement{}
var entLock *sync.RWMutex = &sync.RWMutex{}
var entConfig Config

// RegisterFlags registers the flags
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.GrpcServer, "entitlement.grpc-server", "", "host:port of entitlement server")
	f.StringVar(&cfg.LabelKey, "entitlement.label-key", "", "label key name to check entitlement per orgid/userid")
	f.BoolVar(&cfg.DefaultAllow, "entitlement.default-allow", false, "Allow access if entitlement.label_key doesn't exist")
	f.Var(&cfg.TrustedCnames, "entitlement.trused-cnames", "Trusted CNAMEs")
	f.StringVar(&cfg.UserIDHeader, "entitlement.userid-header", "", "Used as userid instead of CNAME if it's from trusted_cnames")
	f.DurationVar(&cfg.CacheRetainPeriod, "entitlement.cache-retain-period", 1*time.Minute, "")
}

func (e *Entitlement) labelValueFromLabelstring(labelKey string, labelString string) string {
	// labelString format:
	// {agent="curl", filename="/var/tmp/dummy", host="host1.example.com", job="logtest00000999"}

	var re *regexp.Regexp
	var ok bool
	var m []string
	i, ok := e.reLabels.Load(labelKey)
	if !ok {
		re = regexp.MustCompile(labelKey + `="([^"]+)"`)
		e.reLabels.Store(labelKey, re)
	} else {
		re = i.(*regexp.Regexp)
	}
	m = re.FindStringSubmatch(labelString)

	if len(m) > 0 {
		return m[1]
	}
	return ""
}

func (e *Entitlement) entConnect() {
	entLock.Lock()
	defer entLock.Unlock()

	conn, err := grpc.Dial(entConfig.GrpcServer, grpc.WithInsecure())
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "grpc.Dial failed to entServer", "error", err.Error())
		return
	}

	e.entClient = NewEntitlementClient(conn)
}

// SetConfig sets entitlement config
func SetConfig(authzEnabled bool, c Config) {
	entLock.Lock()
	entConfig = c
	ent.authzEnabled = authzEnabled
	entLock.Unlock()
	ent.entConnect()
}

// GetAuthzEnabled returns authz is enabled or not
func GetAuthzEnabled() bool {
	return ent.authzEnabled
}

// Entitled returns true if the action/uid/labelString is entitled by the ent server
func Entitled(action string, oid string, uid string, labelString string) bool {
	// if GrpcServer is not configured, there is no entitlement check
	if !ent.authzEnabled || entConfig.GrpcServer == "" {
		level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("skipping ent check because authzEnabled:%v, GrpcServer:%v", ent.authzEnabled, entConfig.GrpcServer))
		return true
	}

	value := ent.labelValueFromLabelstring(entConfig.LabelKey, labelString)
	if value == "" {
		return entConfig.DefaultAllow
	}
	// 1. entitlement cache
	if entResult, ok := ent.entitledCache(action, oid, uid, labelString); ok {
		if time.Now().Unix()-entResult.timestamp <= int64(entConfig.CacheRetainPeriod.Seconds()) {
			level.Debug(util_log.Logger).Log("msg",
				fmt.Sprintf("Cache found for action:%s, uid:%s, value:%s, entitled:%v, Ts:%v",
					action, uid, value, entResult.entitled, entResult.timestamp))
			return entResult.entitled
		}
		level.Debug(util_log.Logger).Log("msg",
			fmt.Sprintf("Cache expired for action:%s, uid:%s, value:%s, entitled:%v expired, Ts:%v, Now:%v. Talking to entserver",
				action, uid, value, entResult.entitled, entResult.timestamp, time.Now().Unix()))
	} else {
		level.Debug(util_log.Logger).Log("msg",
			fmt.Sprintf("Cache not found for action:%s, uid:%s, value:%s. Talking to entserver", action, uid, value))
	}

	// 2. talk to the entitlement server
	message := &EntitlementRequest{Action: action, LabelValue: value, OrgID: oid, UserID: uid}

	var res *EntitlementResponse
	var err error

	res, err = ent.entClient.Entitled(context.TODO(), message)
	if err != nil {
		ent.entConnect()
		res, err = ent.entClient.Entitled(context.TODO(), message)
	}

	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to connect to entServer", "error", err.Error())
		return false
	}

	// cache it
	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("Cached action:%s, oid:%s, uid:%s, value:%s, entitled:%v", action, oid, uid, value, res.Entitled))
	s := fmt.Sprintf("%s\t%s\t%s\t%s", action, oid, uid, labelString)
	ent.entCache.Store(s, entitlementResult{timestamp: time.Now().Unix(), entitled: res.Entitled})

	return res.Entitled
}

// DeleteCache deletes entitlement cache
func (e *Entitlement) DeleteCache() {
	e.entCache.Range(func(key interface{}, value interface{}) bool {
		e.entCache.Delete(key)
		return true
	})
}

// GetClientUserID returns a client user id
func GetClientUserID(ctx context.Context) (string, error) {
	if ent.authzEnabled {
		// we don't need to check cname here because it's already verified by
		// extractClientUserIDFromGRPCRequest in middleware.go
		return user.ExtractUserID(ctx)
	}
	return "fake", nil
}

// InjectClientUserID injects UserIDHeader into ctx if it's available and from a trusted cname
// otherwise, it injets cname
func InjectClientUserID(ctx *context.Context, r *http.Request) {
	clientUserID := "fake"
	// The leaf certificate is always 0th one in the verified chains
	if ent.authzEnabled && r.TLS != nil && len(r.TLS.VerifiedChains) > 0 && len(r.TLS.VerifiedChains[0]) > 0 {
		cname := r.TLS.VerifiedChains[0][0].Subject.CommonName
		if CnameIsTrusted(cname) {
			level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("CNAME %s is trusted", cname))
			// use the header value as clientUserID if the header is available
			userIDInHeader := clientUserIDInHeader(r)
			level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("userIDInHeader: %s", userIDInHeader))
			if len(userIDInHeader) > 0 {
				clientUserID = userIDInHeader
			} else {
				clientUserID = cname
			}
		} else {
			level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("CNAME %s is not trusted", cname))
			// Otherwise, use cname as clientUserID
			clientUserID = cname
		}
	}
	*ctx = user.InjectUserID(*ctx, clientUserID)
	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("clientUserID= %s injected\n", clientUserID))
}

func CnameIsTrusted(cname string) bool {
	for _, trustedCname := range entConfig.TrustedCnames {
		if cname == trustedCname {
			return true
		}
	}
	return false
}

func clientUserIDInHeader(r *http.Request) string {
	return r.Header.Get(entConfig.UserIDHeader)
}

func (e *Entitlement) entitledCache(action string, oid string, uid string, labelString string) (entitlementResult, bool) {
	s := fmt.Sprintf("%s\t%s\t%s\t%s", action, oid, uid, labelString)
	if item, ok := e.entCache.Load(s); ok {
		return item.(entitlementResult), true
	}
	return entitlementResult{}, false
}

func reLabelsLen() int {
	length := 0
	ent.reLabels.Range(func(_, _ interface{}) bool {
		length++
		return true
	})
	return length
}
