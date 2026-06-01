package dnscheck

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/underundre/unet/internal/wizard"
)

type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
	LookupNS(ctx context.Context, domain string) ([]string, error)
}

type DefaultResolver struct{}

func (d *DefaultResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.LookupHost(host)
}

func (d *DefaultResolver) LookupNS(ctx context.Context, domain string) ([]string, error) {
	nsRecords, err := net.LookupNS(domain)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(nsRecords))
	for i, ns := range nsRecords {
		names[i] = strings.TrimSuffix(ns.Host, ".")
	}
	return names, nil
}

func Validate(ctx context.Context, resolver Resolver, domain string, vpsIP string, port80Free bool) (*wizard.DomainCheckResult, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	result := &wizard.DomainCheckResult{
		Domain:     domain,
		Mode:       "byo",
		CheckedAt:  now,
		Warnings:   []string{},
		Errors:     []string{},
	}

	aRecords, err := resolver.LookupHost(ctx, domain)
	if err != nil {
		result.ARecordIPs = []string{}
		result.Errors = append(result.Errors, fmt.Sprintf("A record lookup failed: %v", err))
	} else {
		result.ARecordIPs = aRecords
		for _, ip := range aRecords {
			if ip == vpsIP {
				result.PointsToVPS = true
				break
			}
		}
	}

	nsRecords, err := resolver.LookupNS(ctx, domain)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("NS lookup failed: %v", err))
	} else {
		for _, ns := range nsRecords {
			if strings.HasSuffix(strings.ToLower(ns), ".cloudflare.com") {
				result.CloudflareDetected = true
				break
			}
		}
	}

	if result.CloudflareDetected && result.CloudflareTokenValid != nil && *result.CloudflareTokenValid {
		result.TLSStrategy = "dns-01"
		result.TLSFeasible = true
	} else if !result.CloudflareDetected {
		if port80Free {
			result.TLSStrategy = "http-01"
			result.TLSFeasible = true
		} else {
			result.TLSStrategy = "http-01"
			result.TLSFeasible = false
			result.Errors = append(result.Errors, "port 80 is blocked and no Cloudflare detected; TLS certificate issuance not possible")
		}
	} else {
		result.TLSStrategy = "dns-01"
		result.TLSFeasible = true
	}

	if !result.PointsToVPS && len(result.ARecordIPs) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("domain does not point to VPS IP %s; current A records: %s", vpsIP, strings.Join(result.ARecordIPs, ", ")))
	}

	return result, nil
}
