package credits

import (
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

	medium, ok := policy.Price(models.CreditActionCreateStory, StoryMediaStats{TotalVideoSeconds: 30})
	if !ok || medium <= base || medium >= maxDynamicStoryCredits {
		t.Fatalf("medium price = %d ok=%v", medium, ok)
	}

	long, ok := policy.Price(models.CreditActionCreateStory, StoryMediaStats{TotalVideoSeconds: 300})
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
