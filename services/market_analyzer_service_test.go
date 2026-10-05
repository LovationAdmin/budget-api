package services

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LovationAdmin/budget-api/models"
)

func TestCacheVariant_SegmentsByPriceBandAndDetails(t *testing.T) {
	if cacheVariant(100, "") != cacheVariant(105, "") {
		t.Fatal("close amounts should share a cache entry")
	}
	if cacheVariant(400, "") == cacheVariant(60, "") {
		t.Fatal("a 400 €/month bill must not reuse offers found for a 60 € one")
	}
	if cacheVariant(100, "") == cacheVariant(100, normalizeDescription("80m², chauffage élec")) {
		t.Fatal("user details must be part of the key")
	}
	if cacheVariant(100, normalizeDescription("  80m²  Chauffage ÉLEC ")) != cacheVariant(100, normalizeDescription("80m² chauffage élec")) {
		t.Fatal("equivalent details should share a cache entry")
	}
}

func TestParseCompetitors_KeepsPricedOffersWithWebsite(t *testing.T) {
	raw := "Voici :\n```json\n" + `{"competitors":[
		{"name":"Sosh","typical_price":10.99,"best_offer":"40 Go","pros":["Sans engagement"],"cons":[],"website_url":"www.sosh.fr"},
		{"name":"NoSite","typical_price":9,"best_offer":"x","pros":[],"cons":[],"website_url":""},
		{"name":"Free","typical_price":0,"best_offer":"x","pros":[],"cons":[],"website_url":"https://free.fr"},
		{"name":"Red","typical_price":12,"best_offer":"x","pros":[],"cons":[],"website_url":"https://www.red-by-sfr.fr","phone_number":"1099"}
	]}` + "\n```"
	got, err := parseCompetitorsFromResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 || got[0].Name != "Sosh" || got[1].Name != "Red" {
		t.Fatalf("got %+v", got)
	}
	if got[0].WebsiteURL != "https://www.sosh.fr" || got[0].AffiliateLink != got[0].WebsiteURL {
		t.Fatalf("website not normalized: %+v", got[0])
	}
	if got[0].ContactAvailable || !got[1].ContactAvailable {
		t.Fatal("contact_available must follow phone/email presence")
	}
}

func TestRecalculateSavings_IndividualChargeCountsEveryMember(t *testing.T) {
	s := &MarketAnalyzerService{}
	// Mobile, 4 people, 80 €/month in total → 20 € per person.
	effective, chargeType := getEffectiveAmount("MOBILE", 80, 4)
	sugg := &models.MarketSuggestion{Competitors: []models.Competitor{
		{Name: "A", TypicalPrice: 10},
		{Name: "B", TypicalPrice: 25}, // dearer than today
		{Name: "C", TypicalPrice: 5},
	}}
	s.recalculateSavings(sugg, effective, 4, chargeType)
	if sugg.Competitors[0].Name != "C" || sugg.Competitors[0].PotentialSavings != 720 {
		t.Fatalf("best = %+v, want C saving (20-5)*12*4 = 720", sugg.Competitors[0])
	}
	if sugg.Competitors[1].PotentialSavings != 480 || sugg.Competitors[2].PotentialSavings != 0 {
		t.Fatalf("got %+v", sugg.Competitors)
	}
}

func TestBuildPrompt_StatesPriceBasisAndHonesty(t *testing.T) {
	s := &MarketAnalyzerService{}
	p := s.buildPrompt("MOBILE", "Orange", 20, "FR", "EUR", 4, ChargeTypeIndividuel, "")
	if !strings.Contains(p, "par personne") || !strings.Contains(p, "liste vide") {
		t.Fatalf("prompt must state the price basis and allow an empty answer:\n%s", p)
	}
	if strings.Contains(p, "potential_savings") {
		t.Fatal("savings are computed server-side, the prompt must not ask for them")
	}
}

func TestFetchOnce_SharesConcurrentCalls(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	fetch := func(context.Context) ([]models.Competitor, error) {
		calls.Add(1)
		<-release
		return []models.Competitor{{Name: "X", TypicalPrice: 1}}, nil
	}

	var wg sync.WaitGroup
	results := make([][]models.Competitor, 5)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = fetchOnce(context.Background(), "same-key", fetch)
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("fetch ran %d times, want 1", calls.Load())
	}
	results[0][0].Name = "mutated"
	if results[1][0].Name != "X" {
		t.Fatal("callers must get independent copies")
	}
}

func TestFetchOnce_CallerCancelDoesNotAbortSharedCall(t *testing.T) {
	done := make(chan struct{})
	fetch := func(ctx context.Context) ([]models.Competitor, error) {
		defer close(done)
		select {
		case <-time.After(100 * time.Millisecond):
			return nil, nil
		case <-ctx.Done():
			t.Error("the shared call must survive the caller's cancellation")
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	if _, err := fetchOnce(ctx, "k2", fetch); err == nil {
		t.Fatal("cancelled caller should get its context error")
	}
	<-done
}
