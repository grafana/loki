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

package main

import (
	"context"
	"flag"
	"fmt"

	grpc "google.golang.org/grpc"

	"github.com/grafana/loki/pkg/entitlement"
)

type entitlementClientOptions struct {
	grpcHost   string
	grpcPort   int
	action     string
	orgid      string
	userid     string
	labelValue string
}

var options entitlementClientOptions = entitlementClientOptions{
	grpcHost: "localhost",
	grpcPort: 21001,
	userid:   "fake",
	orgid:    "fake",
}

func parseFlags() {
	flag.StringVar(&options.grpcHost, "grpcHost", options.grpcHost, "grpcHost")
	flag.IntVar(&options.grpcPort, "grpcPort", options.grpcPort, "gRPC port")
	flag.StringVar(&options.action, "action", options.action, "action {read|write}")
	flag.StringVar(&options.orgid, "orgid", options.userid, "orgid")
	flag.StringVar(&options.userid, "userid", options.userid, "useid")
	flag.StringVar(&options.labelValue, "value", options.labelValue, "label value")
	flag.Parse()
}

func main() {
	parseFlags()
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", options.grpcHost, options.grpcPort), grpc.WithInsecure())
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client := entitlement.NewEntitlementClient(conn)
	message := &entitlement.EntitlementRequest{Action: options.action, LabelValue: options.labelValue, OrgID: options.orgid, UserID: options.userid}

	res, err := client.Entitled(context.TODO(), message)
	if err != nil {
		panic(err)
	}
	if res.Entitled {
		fmt.Println("Entitled")
	} else {
		fmt.Println("Not entitled")
	}
}
