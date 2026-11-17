package facade_open_api

import (
	"math"
	"testing"
)

func TestAIChatPriceForMatchesLongestPrefix(t *testing.T) {
	cases := map[string]float64{
		"gemini-3.5-flash-lite":        2.50, // not the shorter gemini-3.5-flash prefix
		"models/gemini-3.5-flash-lite": 2.50,
		"gemini-3.5-flash":             9.00,
		"claude-sonnet-5-5":            10.00,
		"claude-haiku-5-5-20261007":    0.50,
	}
	for model, output := range cases {
		price, ok := aiChatPriceFor(model)
		if !ok || price.Output != output {
			t.Fatalf("%s: got %+v ok=%v", model, price, ok)
		}
	}
	if _, ok := aiChatPriceFor("some-new-model"); ok {
		t.Fatal("unknown model must be unpriced")
	}
}

func TestAIChatUsageTotalsCost(t *testing.T) {
	zero := 0.0
	total := AIChatUsageTotals{CostUSD: &zero}
	price, _ := aiChatPriceFor("gemini-3.5-flash-lite")
	cost := 1e6/1e6*price.Input + 2e5/1e6*price.Output // 0.30 + 0.50
	total.add(3, 5, 1_000_000, 200_000, cost, true, false)
	if total.CostUSD == nil || math.Abs(*total.CostUSD-0.80) > 1e-9 || total.InputTokens != 1_000_000 || total.Replies != 3 {
		t.Fatalf("priced total %+v", total)
	}
	total.add(1, 1, 10, 10, 0, false, false)
	total.add(1, 1, 10, 10, 1, true, false)
	if total.CostUSD != nil {
		t.Fatal("an unpriced model makes the total cost unknown")
	}
	bucket := AIChatUsageTotals{}
	bucket.add(1, 1, 1, 1, 0.25, true, true)
	if bucket.CostUSD == nil || *bucket.CostUSD != 0.25 {
		t.Fatalf("first priced row sets the cost: %+v", bucket)
	}
}
