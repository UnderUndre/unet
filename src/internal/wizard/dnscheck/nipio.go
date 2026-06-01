package dnscheck

import (
	"context"
	"fmt"
	"strings"
)

func BuildNipioSubdomain(label string, vpsPublicIP string) string {
	dashed := strings.ReplaceAll(vpsPublicIP, ".", "-")
	if label == "" {
		return dashed + ".nip.io"
	}
	return label + "." + dashed + ".nip.io"
}

func CheckNipioResolution(ctx context.Context, resolver Resolver, vpsPublicIP string) (works bool, warning string) {
	dashed := strings.ReplaceAll(vpsPublicIP, ".", "-")
	testDomain := dashed + ".nip.io"

	ips, err := resolver.LookupHost(ctx, testDomain)
	if err != nil {
		return false, "nip.io DNS unreachable. Proceed without DNS verification or use your own domain."
	}

	for _, ip := range ips {
		if ip == vpsPublicIP {
			return true, ""
		}
	}

	return false, fmt.Sprintf("nip.io resolved %s but returned unexpected IPs: %v", testDomain, ips)
}
