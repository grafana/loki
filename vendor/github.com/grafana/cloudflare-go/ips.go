package cloudflare

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/pkg/errors"
)

// IPRangesResponse contains the structure for the API response, not modified.
type IPRangesResponse struct {
	IPv4CIDRs  []string `json:"ipv4_cidrs"`
	IPv6CIDRs  []string `json:"ipv6_cidrs"`
	ChinaColos []string `json:"china_colos"`
}

// IPRanges contains lists of IPv4 and IPv6 CIDRs.
type IPRanges struct {
	IPv4CIDRs      []string `json:"ipv4_cidrs"`
	IPv6CIDRs      []string `json:"ipv6_cidrs"`
	ChinaIPv4CIDRs []string `json:"china_ipv4_cidrs"`
	ChinaIPv6CIDRs []string `json:"china_ipv6_cidrs"`
}

// IPsResponse is the API response containing a list of IPs.
type IPsResponse struct {
	Response
	Result IPRangesResponse `json:"result"`
}

// IPs gets a list of Cloudflare's IP ranges.
//
// This does not require logging in to the API.
//
// API reference: https://api.cloudflare.com/#cloudflare-ips
func IPs() (IPRanges, error) {
	uri := fmt.Sprintf("%s/ips?china_colo=1", apiURL)
	resp, err := http.Get(uri)
	if err != nil {
		return IPRanges{}, errors.Wrap(err, "HTTP request failed")
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return IPRanges{}, errors.Wrap(err, "Response body could not be read")
	}
	var r IPsResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return IPRanges{}, errors.Wrap(err, errUnmarshalError)
	}

	var ips IPRanges
	ips.IPv4CIDRs = r.Result.IPv4CIDRs
	ips.IPv6CIDRs = r.Result.IPv6CIDRs

	for _, ip := range r.Result.ChinaColos {
		if strings.Contains(ip, ":") {
			ips.ChinaIPv6CIDRs = append(ips.ChinaIPv6CIDRs, ip)
		} else {
			ips.ChinaIPv4CIDRs = append(ips.ChinaIPv4CIDRs, ip)
		}
	}

	return ips, nil
}
