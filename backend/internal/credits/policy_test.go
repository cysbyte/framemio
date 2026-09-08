package credits

import (
	"encoding/json"
	"testing"

	"github.com/framemio/backend/internal/models"
)

func TestPolicyPriceUsesDynamicStoryCurveWhenEnabled(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.DynamicStoryPricing = true

	base, ok := policy.Price(models.CreditActionCreateStory, StoryMediaStats{})
	if !ok || base != 40 {
		t.Fatalf("base price = %d ok=%v", base, ok)
	}

	short, ok := policy.Price(models.CreditActionCreateStory, StoryMediaStats{TotalVideoSeconds: 119})
	if !ok || short != base {
		t.Fatalf("short price = %d ok=%v", short, ok)
	}

	threshold, ok := policy.Price(models.CreditActionCreateStory, StoryMediaStats{TotalVideoSeconds: dynamicStoryPricingThresholdSeconds})
	if !ok || threshold != base {
		t.Fatalf("threshold price = %d ok=%v", threshold, ok)
	}

	medium, ok := policy.Price(models.CreditActionCreateStory, StoryMediaStats{TotalVideoSeconds: 150})
	if !ok || medium <= base || medium >= maxDynamicStoryCredits {
		t.Fatalf("medium price = %d ok=%v", medium, ok)
	}

	long, ok := policy.Price(models.CreditActionCreateStory, StoryMediaStats{TotalVideoSeconds: 420})
	if !ok || long != maxDynamicStoryCredits {
		t.Fatalf("long price = %d ok=%v", long, ok)
	}
}

func TestPolicyPriceUsesStaticFallbackWhenDisabled(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	policy.DynamicStoryPricing = false

	price, ok := policy.Price(models.CreditActionRegenerateStory, StoryMediaStats{StoryVideoSeconds: 300})
	if !ok || price != 40 {
		t.Fatalf("price = %d ok=%v", price, ok)
	}
}

func TestLoadPolicyFromEnvCanToggleDynamicStoryPricing(t *testing.T) {
	t.Setenv("CREDIT_DYNAMIC_STORY_PRICING", "true")
	policy := LoadPolicyFromEnv()
	if !policy.DynamicStoryPricing {
		t.Fatal("expected dynamic story pricing to be enabled")
	}
}

func TestStoryMediaStatsDecodesJSONQuotePayload(t *testing.T) {
	t.Parallel()

	var stats StoryMediaStats
	if err := json.Unmarshal([]byte(`{"total_video_seconds":300,"total_media_count":5,"video_count":2}`), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.TotalVideoSeconds != 300 || stats.TotalMediaCount != 5 || stats.VideoCount != 2 {
		t.Fatalf("stats = %+v", stats)
	}
}
