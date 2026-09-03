package credits

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/framemio/backend/internal/models"
)

type Limits struct {
	CreateStoryMaxPhotos          int
	CreateStoryMaxVideoSeconds    int
	CreateStoryMaxMedia           int
	AddMomentMaxPhotos            int
	AddMomentMaxVideoSeconds      int
	AddMomentMaxMedia             int
	RegenerateStoryMaxMedia       int
	RegenerateStoryMaxDurationSec int
	AIEditMaxMedia                int
	AIEditMaxDurationSec          int
}

type CreditPack struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	ProductID string `json:"product_id,omitempty"`
	Credits   int    `json:"credits"`
}

type Policy struct {
	Prices                          map[models.CreditAction]int
	Limits                          Limits
	InitialGrant                    int
	MonthlySubscriptionCycleCredits int
	YearlySubscriptionCycleCredits  int
	CreditPacks                     []CreditPack
}

func DefaultPolicy() Policy {
	return Policy{
		Prices: map[models.CreditAction]int{
			models.CreditActionCreateStory:     40,
			models.CreditActionRegenerateStory: 40,
			models.CreditActionAddMoment:       20,
			models.CreditActionAIEditStory:     20,
			models.CreditActionChangeMusic:     0,
			models.CreditActionChangeCaption:   0,
			models.CreditActionReorderMoment:   0,
			models.CreditActionRemoveMoment:    0,
			models.CreditActionPreview:         0,
		},
		Limits: Limits{
			CreateStoryMaxPhotos:          20,
			CreateStoryMaxVideoSeconds:    120,
			CreateStoryMaxMedia:           20,
			AddMomentMaxPhotos:            10,
			AddMomentMaxVideoSeconds:      60,
			AddMomentMaxMedia:             10,
			RegenerateStoryMaxMedia:       20,
			RegenerateStoryMaxDurationSec: 30,
			AIEditMaxMedia:                20,
			AIEditMaxDurationSec:          30,
		},
		InitialGrant:                    100,
		MonthlySubscriptionCycleCredits: 300,
		YearlySubscriptionCycleCredits:  360,
		CreditPacks: []CreditPack{
			{ID: "small", Title: "Small Pack", Subtitle: "A few more story moments", ProductID: "com.framemio.credits.small", Credits: 100},
			{ID: "medium", Title: "Medium Pack", Subtitle: "More room for memories", ProductID: "com.framemio.credits.medium", Credits: 300},
			{ID: "large", Title: "Large Pack", Subtitle: "Keep creating all week", ProductID: "com.framemio.credits.large", Credits: 700},
		},
	}
}

func LoadPolicyFromEnv() Policy {
	policy := DefaultPolicy()
	for action, envName := range map[models.CreditAction]string{
		models.CreditActionCreateStory:     "CREDIT_PRICE_CREATE_STORY",
		models.CreditActionRegenerateStory: "CREDIT_PRICE_REGENERATE_STORY",
		models.CreditActionAddMoment:       "CREDIT_PRICE_ADD_MOMENT",
		models.CreditActionAIEditStory:     "CREDIT_PRICE_AI_EDIT_STORY",
		models.CreditActionChangeMusic:     "CREDIT_PRICE_CHANGE_MUSIC",
		models.CreditActionChangeCaption:   "CREDIT_PRICE_CHANGE_CAPTION",
		models.CreditActionReorderMoment:   "CREDIT_PRICE_REORDER_MOMENT",
		models.CreditActionRemoveMoment:    "CREDIT_PRICE_REMOVE_MOMENT",
		models.CreditActionPreview:         "CREDIT_PRICE_PREVIEW",
	} {
		if value, ok := readIntEnv(envName); ok {
			policy.Prices[action] = value
		}
	}
	if value, ok := readIntEnv("CREDIT_INITIAL_GRANT"); ok {
		policy.InitialGrant = value
	}
	if value, ok := readIntEnv("CREDIT_SUBSCRIPTION_MONTHLY_CYCLE_CREDITS"); ok {
		policy.MonthlySubscriptionCycleCredits = value
	}
	if value, ok := readIntEnv("CREDIT_SUBSCRIPTION_YEARLY_CYCLE_CREDITS"); ok {
		policy.YearlySubscriptionCycleCredits = value
	}
	policy.CreditPacks = loadCreditPacksFromEnv(policy.CreditPacks)
	policy.Limits.CreateStoryMaxPhotos = readIntEnvOrDefault("CREDIT_CREATE_STORY_MAX_PHOTOS", policy.Limits.CreateStoryMaxPhotos)
	policy.Limits.CreateStoryMaxVideoSeconds = readIntEnvOrDefault("CREDIT_CREATE_STORY_MAX_VIDEO_SECONDS", policy.Limits.CreateStoryMaxVideoSeconds)
	policy.Limits.CreateStoryMaxMedia = readIntEnvOrDefault("CREDIT_CREATE_STORY_MAX_MEDIA", policy.Limits.CreateStoryMaxMedia)
	policy.Limits.AddMomentMaxPhotos = readIntEnvOrDefault("CREDIT_ADD_MOMENT_MAX_PHOTOS", policy.Limits.AddMomentMaxPhotos)
	policy.Limits.AddMomentMaxVideoSeconds = readIntEnvOrDefault("CREDIT_ADD_MOMENT_MAX_VIDEO_SECONDS", policy.Limits.AddMomentMaxVideoSeconds)
	policy.Limits.AddMomentMaxMedia = readIntEnvOrDefault("CREDIT_ADD_MOMENT_MAX_MEDIA", policy.Limits.AddMomentMaxMedia)
	policy.Limits.RegenerateStoryMaxMedia = readIntEnvOrDefault("CREDIT_REGENERATE_STORY_MAX_MEDIA", policy.Limits.RegenerateStoryMaxMedia)
	policy.Limits.RegenerateStoryMaxDurationSec = readIntEnvOrDefault("CREDIT_REGENERATE_STORY_MAX_DURATION_SECONDS", policy.Limits.RegenerateStoryMaxDurationSec)
	policy.Limits.AIEditMaxMedia = readIntEnvOrDefault("CREDIT_AI_EDIT_MAX_MEDIA", policy.Limits.AIEditMaxMedia)
	policy.Limits.AIEditMaxDurationSec = readIntEnvOrDefault("CREDIT_AI_EDIT_MAX_DURATION_SECONDS", policy.Limits.AIEditMaxDurationSec)
	return policy
}

func (p Policy) Price(action models.CreditAction) (int, bool) {
	price, ok := p.Prices[action]
	return price, ok
}

func (p Policy) CreditPack(id string) (CreditPack, bool) {
	normalizedID := strings.ToLower(strings.TrimSpace(id))
	for _, pack := range p.CreditPacks {
		if strings.ToLower(pack.ID) == normalizedID || (pack.ProductID != "" && strings.ToLower(pack.ProductID) == normalizedID) {
			return pack, true
		}
	}
	return CreditPack{}, false
}

func (p Policy) Quote(action models.CreditAction, balance int, eligible bool, reason string) models.CreditQuote {
	price, _ := p.Price(action)
	quote := models.CreditQuote{Action: action, Credits: price, Balance: balance, BalanceAfter: balance - price, Eligible: eligible}
	if reason != "" {
		quote.Reason = reason
	}
	return quote
}

type StoryMediaStats struct {
	PhotoCount         int
	VideoCount         int
	TotalMediaCount    int
	TotalVideoSeconds  int
	TotalBytes         int64
	StoryMediaCount    int
	StoryVideoSeconds  int
	AddedPhotoCount    int
	AddedVideoSeconds  int
	AddedMediaCount    int
	HasAIInstruction   bool
	HasCaptionOnlyEdit bool
}

func (p Policy) Validate(action models.CreditAction, stats StoryMediaStats) (bool, string) {
	switch action {
	case models.CreditActionCreateStory:
		if stats.TotalMediaCount > p.Limits.CreateStoryMaxMedia {
			return false, fmt.Sprintf("This action supports up to %d media items.", p.Limits.CreateStoryMaxMedia)
		}
		if stats.PhotoCount > p.Limits.CreateStoryMaxPhotos {
			return false, fmt.Sprintf("This action supports up to %d photos.", p.Limits.CreateStoryMaxPhotos)
		}
		if stats.TotalVideoSeconds > p.Limits.CreateStoryMaxVideoSeconds {
			return false, fmt.Sprintf("This action supports up to %d seconds of video.", p.Limits.CreateStoryMaxVideoSeconds)
		}
	case models.CreditActionAddMoment:
		if stats.AddedMediaCount > p.Limits.AddMomentMaxMedia {
			return false, fmt.Sprintf("This action supports up to %d added media items.", p.Limits.AddMomentMaxMedia)
		}
		if stats.AddedPhotoCount > p.Limits.AddMomentMaxPhotos {
			return false, fmt.Sprintf("This action supports up to %d added photos.", p.Limits.AddMomentMaxPhotos)
		}
		if stats.AddedVideoSeconds > p.Limits.AddMomentMaxVideoSeconds {
			return false, fmt.Sprintf("This action supports up to %d seconds of added video.", p.Limits.AddMomentMaxVideoSeconds)
		}
	case models.CreditActionRegenerateStory:
		if stats.StoryMediaCount > p.Limits.RegenerateStoryMaxMedia {
			return false, fmt.Sprintf("This action supports up to %d media items in the story.", p.Limits.RegenerateStoryMaxMedia)
		}
		if stats.StoryVideoSeconds > p.Limits.RegenerateStoryMaxDurationSec {
			return false, fmt.Sprintf("This action supports up to %d seconds of story duration.", p.Limits.RegenerateStoryMaxDurationSec)
		}
	case models.CreditActionAIEditStory:
		if stats.StoryMediaCount > p.Limits.AIEditMaxMedia {
			return false, fmt.Sprintf("This action supports up to %d media items in the story.", p.Limits.AIEditMaxMedia)
		}
		if stats.StoryVideoSeconds > p.Limits.AIEditMaxDurationSec {
			return false, fmt.Sprintf("This action supports up to %d seconds of story duration.", p.Limits.AIEditMaxDurationSec)
		}
	}
	return true, ""
}

func readIntEnv(name string) (int, bool) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

func readIntEnvOrDefault(name string, fallback int) int {
	if value, ok := readIntEnv(name); ok {
		return value
	}
	return fallback
}

func loadCreditPacksFromEnv(defaults []CreditPack) []CreditPack {
	packs := append([]CreditPack(nil), defaults...)
	for index, key := range []string{"SMALL", "MEDIUM", "LARGE"} {
		if index >= len(packs) {
			continue
		}
		if value := strings.TrimSpace(os.Getenv("CREDIT_PACK_" + key + "_ID")); value != "" {
			packs[index].ID = value
		}
		if value := strings.TrimSpace(os.Getenv("CREDIT_PACK_" + key + "_TITLE")); value != "" {
			packs[index].Title = value
		}
		if value := strings.TrimSpace(os.Getenv("CREDIT_PACK_" + key + "_SUBTITLE")); value != "" {
			packs[index].Subtitle = value
		}
		if value := strings.TrimSpace(os.Getenv("CREDIT_PACK_" + key + "_PRODUCT_ID")); value != "" {
			packs[index].ProductID = value
		}
		if value, ok := readIntEnv("CREDIT_PACK_" + key + "_CREDITS"); ok {
			packs[index].Credits = value
		}
	}
	return packs
}
