package awspricing

import (
	"testing"
)

func TestPriceSelectionPreservesLowestPositiveUSD(t *testing.T) {
	prices := []string{
		"invalid JSON",
		`{"terms":{"OnDemand":{"offer":{"priceDimensions":{"free":{"pricePerUnit":{"USD":"0"}},"invalid":{"pricePerUnit":{"USD":"unknown"}},"hourly":{"pricePerUnit":{"USD":"0.42"}},"lower":{"pricePerUnit":{"USD":"0.12"}},"other-currency":{"pricePerUnit":{"EUR":"0.01"}}}}}}}`,
	}
	got, err := extractUSDPriceFromPricingResult(prices)
	if err != nil || got != 0.12 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestMissingUSDPriceIsAnError(t *testing.T) {
	for _, prices := range [][]string{nil, {"invalid"}, {`{"terms":{"OnDemand":{}}}`}} {
		if _, err := extractUSDPriceFromPricingResult(prices); err == nil {
			t.Fatal("missing price accepted")
		}
	}
}

func TestPricingLocationMapping(t *testing.T) {
	for region, want := range map[string]string{"us-east-1": "US East (N. Virginia)", "us-east-2": "US East (Ohio)", "us-west-1": "US West (N. California)", "us-west-2": "US West (Oregon)"} {
		got, err := awsPricingLocation(region)
		if err != nil || got != want {
			t.Fatalf("%s: got %q, %v", region, got, err)
		}
	}
	if _, err := awsPricingLocation("unmapped-region"); err == nil {
		t.Fatal("unmapped region accepted")
	}
}
