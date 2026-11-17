package facade_open_api

import (
	"context"
	"sort"
	"strings"
	"time"
)

// aiChatPrice is a list price in USD per 1M tokens. Estimates only: they ignore free
// tiers, batch and cache discounts, and Claude Haiku 5.5's higher rate for prompts over
// 100K tokens (usage is stored per reply, not per call).
type aiChatPrice struct {
	Input, Output float64
}

// aiChatPriceSource dates the table below; update both when providers change prices.
const aiChatPriceSource = "ราคาประกาศ (list price) ต่อ 1M tokens ตรวจเมื่อ 10 พ.ย. 2026: ai.google.dev/gemini-api/docs/pricing และ claude.com/pricing"

// Longest prefix wins, so dated or suffixed model ids still match.
var aiChatPrices = map[string]aiChatPrice{
	"gemini-3.5-flash-lite": {0.30, 2.50},
	"gemini-3.5-flash":      {1.50, 9.00},
	"gemini-3.1-flash-lite": {0.25, 1.50},
	"gemini-3-flash":        {0.50, 3.00},
	"gemini-3.1-pro":        {2.00, 12.00},
	"claude-haiku-5-5":      {0.10, 0.50},
	"claude-sonnet-5-5":     {2.00, 10.00},
	"claude-opus-5-5":       {4.00, 20.00},
	"claude-fable-5-1":      {10.00, 50.00},
}

func aiChatPriceFor(model string) (aiChatPrice, bool) {
	model = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(model), "models/"))
	best, found := "", false
	for prefix := range aiChatPrices {
		if strings.HasPrefix(model, prefix) && len(prefix) > len(best) {
			best, found = prefix, true
		}
	}
	return aiChatPrices[best], found
}

type AIChatUsageTotals struct {
	Replies      int64    `json:"replies"`
	Steps        int64    `json:"steps"`
	InputTokens  int64    `json:"inputTokens"`
	OutputTokens int64    `json:"outputTokens"`
	CostUSD      *float64 `json:"costUsd"`
}
type AIChatUsageModel struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	InputPrice  *float64 `json:"inputPricePerMTok"`
	OutputPrice *float64 `json:"outputPricePerMTok"`
	AIChatUsageTotals
}
type AIChatUsageDay struct {
	Day string `json:"day"`
	AIChatUsageTotals
}
type AIChatUsageProject struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	AIChatUsageTotals
}
type AIChatUsageDTO struct {
	Days          int                  `json:"days"`
	Since         string               `json:"since"`
	PriceSource   string               `json:"priceSource"`
	UnpricedModel bool                 `json:"unpricedModel"`
	Total         AIChatUsageTotals    `json:"total"`
	Models        []AIChatUsageModel   `json:"models"`
	ByDay         []AIChatUsageDay     `json:"byDay"`
	Projects      []AIChatUsageProject `json:"projects"`
}

// add sums a bucket; a cost of nil means some tokens in it have no known price.
func (t *AIChatUsageTotals) add(replies, steps, in, out int64, cost float64, priced bool, first bool) {
	t.Replies += replies
	t.Steps += steps
	t.InputTokens += in
	t.OutputTokens += out
	switch {
	case !priced:
		t.CostUSD = nil
	case first || t.CostUSD != nil:
		v := cost
		if t.CostUSD != nil {
			v += *t.CostUSD
		}
		t.CostUSD = &v
	}
}

// Usage reports the user's own AI chat token usage over the last `days` days (1–365).
func (f *VideoEditorAIChatFacade) Usage(ctx context.Context, user string, days int) (AIChatUsageDTO, error) {
	if days < 1 || days > 365 {
		days = 30
	}
	bangkok := time.FixedZone("Asia/Bangkok", 7*3600)
	now := time.Now().In(bangkok)
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, bangkok).AddDate(0, 0, -(days - 1))
	out := AIChatUsageDTO{Days: days, Since: since.Format("2006-01-02"), PriceSource: aiChatPriceSource, Models: []AIChatUsageModel{}, ByDay: []AIChatUsageDay{}, Projects: []AIChatUsageProject{}}
	if !veID(user) {
		return out, nil
	}
	zero := 0.0
	out.Total.CostUSD = &zero
	if f.repo == nil || !f.repo.Ready(ctx) {
		return out, nil
	}
	rows, e := f.repo.Usage(ctx, user, since)
	if e != nil {
		return out, e
	}
	models, daysAt, projects := map[string]int{}, map[string]int{}, map[string]int{}
	for _, r := range rows {
		price, priced := aiChatPriceFor(r.Model)
		cost := float64(r.InputTokens)/1e6*price.Input + float64(r.OutputTokens)/1e6*price.Output
		if !priced {
			out.UnpricedModel = true
		}
		key := r.Provider + "\x00" + r.Model
		i, ok := models[key]
		if !ok {
			m := AIChatUsageModel{Provider: r.Provider, Model: r.Model}
			if priced {
				in, o := price.Input, price.Output
				m.InputPrice, m.OutputPrice = &in, &o
			}
			out.Models = append(out.Models, m)
			i = len(out.Models) - 1
			models[key] = i
		}
		out.Models[i].add(r.Replies, r.Steps, r.InputTokens, r.OutputTokens, cost, priced, !ok)
		d, ok := daysAt[r.Day]
		if !ok {
			out.ByDay = append(out.ByDay, AIChatUsageDay{Day: r.Day})
			d = len(out.ByDay) - 1
			daysAt[r.Day] = d
		}
		out.ByDay[d].add(r.Replies, r.Steps, r.InputTokens, r.OutputTokens, cost, priced, !ok)
		p, ok := projects[r.ProjectID]
		if !ok {
			out.Projects = append(out.Projects, AIChatUsageProject{ProjectID: r.ProjectID, Title: r.ProjectTitle})
			p = len(out.Projects) - 1
			projects[r.ProjectID] = p
		}
		out.Projects[p].add(r.Replies, r.Steps, r.InputTokens, r.OutputTokens, cost, priced, !ok)
		out.Total.add(r.Replies, r.Steps, r.InputTokens, r.OutputTokens, cost, priced, false)
	}
	sort.Slice(out.ByDay, func(i, j int) bool { return out.ByDay[i].Day > out.ByDay[j].Day })
	tokens := func(t AIChatUsageTotals) int64 { return t.InputTokens + t.OutputTokens }
	sort.SliceStable(out.Models, func(i, j int) bool {
		return tokens(out.Models[i].AIChatUsageTotals) > tokens(out.Models[j].AIChatUsageTotals)
	})
	sort.SliceStable(out.Projects, func(i, j int) bool {
		return tokens(out.Projects[i].AIChatUsageTotals) > tokens(out.Projects[j].AIChatUsageTotals)
	})
	return out, nil
}
