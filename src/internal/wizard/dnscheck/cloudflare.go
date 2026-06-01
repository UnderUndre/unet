package dnscheck

import (
	"context"
	"fmt"
	"strings"
)

type Zone struct {
	ID   string
	Name string
}

type CloudflareAPI interface {
	ListZones(ctx context.Context) ([]Zone, error)
	VerifyToken(ctx context.Context) (bool, []string, error)
}

type CFValidationError struct {
	Code    string
	Message string
}

func (e *CFValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func ValidateCloudflareToken(ctx context.Context, api CloudflareAPI, token string, domain string) (valid bool, scopes []string, zoneID string, err error) {
	if token == "" {
		return false, nil, "", &CFValidationError{
			Code:    "cf_token_empty",
			Message: "cloudflare token is required",
		}
	}

	valid, scopes, err = api.VerifyToken(ctx)
	if err != nil {
		return false, nil, "", &CFValidationError{
			Code:    "cf_token_invalid",
			Message: fmt.Sprintf("token verification failed: %v", err),
		}
	}

	if !valid {
		return false, nil, "", &CFValidationError{
			Code:    "cf_token_invalid",
			Message: "token is not valid",
		}
	}

	hasRequiredScope := false
	for _, s := range scopes {
		if strings.HasPrefix(s, "Zone:DNS:") || s == "Zone:DNS:Edit" || s == "Zone:Edit" {
			hasRequiredScope = true
			break
		}
	}
	if !hasRequiredScope {
		return true, scopes, "", &CFValidationError{
			Code:    "cf_token_missing_scope",
			Message: "token lacks required DNS edit scope",
		}
	}

	zones, err := api.ListZones(ctx)
	if err != nil {
		return true, scopes, "", &CFValidationError{
			Code:    "cf_zone_list_failed",
			Message: fmt.Sprintf("failed to list zones: %v", err),
		}
	}

	for _, z := range zones {
		if z.Name == domain {
			return true, scopes, z.ID, nil
		}
		if strings.HasSuffix(domain, "."+z.Name) {
			return true, scopes, z.ID, nil
		}
	}

	return true, scopes, "", &CFValidationError{
		Code:    "cf_zone_not_found",
		Message: fmt.Sprintf("no cloudflare zone found for domain %s", domain),
	}
}
