package ai

import (
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
)

// EditStrategy applies project-mode editorial intent on top of the shared
// Media -> Analysis -> Candidates -> StoryPlan -> Render pipeline.
type EditStrategy interface {
	Name() string
	ProjectType() models.ProjectType
	Objective() string
	PromptGuidance() string
	SelectCandidates([]candidate, time.Duration) []candidate
	SortCandidates([]candidate, Request)
	ApplyMomentDefaults(Request, []models.StoryMoment)
	ApplyPlanDefaults(*models.StoryPlan, Request)
	ApplyRevision(*models.StoryPlan, StoryRevisionInput) []StoryRevisionChange
	UnsupportedInstructionWarnings(string) []string
}

func EditStrategyForProjectType(projectType models.ProjectType) EditStrategy {
	projectType, valid := models.NormalizeProjectType(projectType)
	if !valid {
		projectType = models.ProjectTypeStory
	}
	switch projectType {
	case models.ProjectTypePromo:
		return PromoEditStrategy{}
	case models.ProjectTypeReel:
		return ReelEditStrategy{}
	default:
		return StoryEditStrategy{}
	}
}

func EditStrategyForRequest(request Request) EditStrategy {
	return EditStrategyForProjectType(request.ProjectType)
}

type StoryEditStrategy struct{}
type PromoEditStrategy struct{}
type ReelEditStrategy struct{}

func (StoryEditStrategy) Name() string                    { return "story" }
func (StoryEditStrategy) ProjectType() models.ProjectType { return models.ProjectTypeStory }
func (StoryEditStrategy) Objective() string {
	return "Create a coherent personal story from everyday media."
}
func (StoryEditStrategy) PromptGuidance() string {
	return "Optimize for chronological/contextual coherence, important moments, emotional progression, variety, natural pacing, and a meaningful ending. Avoid excessive effects."
}
func (StoryEditStrategy) SelectCandidates(candidates []candidate, desired time.Duration) []candidate {
	return selectCandidates(candidates, desired)
}
func (StoryEditStrategy) SortCandidates(candidates []candidate, request Request) {
	sortCandidatesForStory(candidates, request.PreferredMediaOrder)
}
func (StoryEditStrategy) ApplyMomentDefaults(Request, []models.StoryMoment) {}
func (StoryEditStrategy) ApplyPlanDefaults(plan *models.StoryPlan, request Request) {
	if plan == nil {
		return
	}
	plan.ProjectType = models.ProjectTypeStory
	if plan.Output.AspectRatio == "" {
		plan.Output.AspectRatio = projectAspectRatio(request)
	}
}
func (StoryEditStrategy) ApplyRevision(plan *models.StoryPlan, input StoryRevisionInput) []StoryRevisionChange {
	if plan == nil {
		return nil
	}
	return replaceOpeningIfRequested(plan, input)
}
func (StoryEditStrategy) UnsupportedInstructionWarnings(instruction string) []string {
	return unsupportedInstructionWarnings(instruction)
}

func (PromoEditStrategy) Name() string                    { return "promo" }
func (PromoEditStrategy) ProjectType() models.ProjectType { return models.ProjectTypePromo }
func (PromoEditStrategy) Objective() string {
	return "Create a persuasive promotional video."
}
func (PromoEditStrategy) PromptGuidance() string {
	return "Optimize for hook, product visibility, benefits, proof/demo, clear messaging, CTA, and brand consistency. A useful arc is HOOK -> PROBLEM -> PRODUCT -> BENEFIT -> PROOF -> CTA, but omit stages that do not apply."
}
func (PromoEditStrategy) SelectCandidates(candidates []candidate, desired time.Duration) []candidate {
	return selectCandidates(candidates, desired)
}
func (PromoEditStrategy) SortCandidates(candidates []candidate, request Request) {
	sortCandidatesForStory(candidates, request.PreferredMediaOrder)
	if len(request.PreferredMediaOrder) > 0 || len(candidates) < 2 {
		return
	}
	best := 0
	for index := range candidates {
		if candidates[index].score > candidates[best].score {
			best = index
		}
	}
	if best > 0 {
		opening := candidates[best]
		copy(candidates[1:best+1], candidates[0:best])
		candidates[0] = opening
	}
}
func (PromoEditStrategy) ApplyMomentDefaults(request Request, moments []models.StoryMoment) {
	applyProjectSceneDefaults(request, moments)
}
func (PromoEditStrategy) ApplyPlanDefaults(plan *models.StoryPlan, request Request) {
	if plan == nil {
		return
	}
	plan.ProjectType = models.ProjectTypePromo
	if cta := projectCallToAction(request); cta != "" && strings.TrimSpace(plan.CallToAction) == "" {
		plan.CallToAction = cta
		if strings.TrimSpace(plan.EndingText) == "" {
			plan.EndingText = cta
		}
	}
}
func (PromoEditStrategy) ApplyRevision(plan *models.StoryPlan, input StoryRevisionInput) []StoryRevisionChange {
	if plan == nil {
		return nil
	}
	changes := replaceOpeningIfRequested(plan, input)
	if wantsStrongerCTA(strings.ToLower(input.UserInstruction)) {
		cta := firstNonEmptyRevision(projectCallToAction(Request{ProjectType: models.ProjectTypePromo, ProjectConfiguration: input.Story.ProjectConfiguration}), plan.CallToAction, "Learn more")
		if !strings.Contains(strings.ToLower(cta), "today") {
			cta = strings.TrimSpace(cta + " today")
		}
		plan.CallToAction = cta
		plan.EndingText = cta
		if len(plan.Moments) > 0 {
			last := &plan.Moments[len(plan.Moments)-1]
			last.ScenePurpose = models.PromoScenePurposeCTA
			if last.Typography == nil {
				last.Typography = promoTypographyForScene(Request{ProjectType: models.ProjectTypePromo, ProjectConfiguration: input.Story.ProjectConfiguration}, *last)
			}
			if last.Typography != nil {
				last.Typography.Title = cta
				last.Typography.Type = models.TextOverlayPromoCTA
				last.Typography.Placement = models.TextPlacementBottomCenter
			}
		}
		changes = append(changes, StoryRevisionChange{Type: "cta_change", Target: "call_to_action", Reason: "strengthened CTA-related portion"})
	}
	return changes
}
func (PromoEditStrategy) UnsupportedInstructionWarnings(instruction string) []string {
	return unsupportedInstructionWarnings(instruction)
}

func (ReelEditStrategy) Name() string                    { return "reel" }
func (ReelEditStrategy) ProjectType() models.ProjectType { return models.ProjectTypeReel }
func (ReelEditStrategy) Objective() string {
	return "Create an engaging short-form social video."
}
func (ReelEditStrategy) PromptGuidance() string {
	return "Optimize for strong opening, concise storytelling, pacing, retention, captions, vertical framing, and a clear ending. Do not use viral as a guaranteed outcome."
}
func (ReelEditStrategy) SelectCandidates(candidates []candidate, desired time.Duration) []candidate {
	return selectCandidates(candidates, desired)
}
func (ReelEditStrategy) SortCandidates(candidates []candidate, request Request) {
	sortCandidatesForStory(candidates, request.PreferredMediaOrder)
	if len(request.PreferredMediaOrder) > 0 || len(candidates) < 2 {
		return
	}
	best := 0
	for index := range candidates {
		if candidates[index].score > candidates[best].score {
			best = index
		}
	}
	if best > 0 {
		opening := candidates[best]
		copy(candidates[1:best+1], candidates[0:best])
		candidates[0] = opening
	}
}
func (ReelEditStrategy) ApplyMomentDefaults(request Request, moments []models.StoryMoment) {
	applyReelSceneDefaults(request, moments)
}
func (ReelEditStrategy) ApplyPlanDefaults(plan *models.StoryPlan, request Request) {
	if plan == nil {
		return
	}
	plan.ProjectType = models.ProjectTypeReel
	plan.Output.AspectRatio = "9:16"
	for index := range plan.Moments {
		plan.Moments[index].Order = index + 1
		plan.Moments[index].CropIntent = "fill"
		plan.Moments[index].ScenePurpose = ""
		plan.Moments[index].SceneText = nil
		plan.Moments[index].Voiceover = nil
	}
	for index := range plan.BRoll {
		plan.BRoll[index].CropIntent = "fill"
	}
}
func (ReelEditStrategy) ApplyRevision(plan *models.StoryPlan, input StoryRevisionInput) []StoryRevisionChange {
	if plan == nil {
		return nil
	}
	changes := replaceOpeningIfRequested(plan, input)
	if wantsCaptionReadability(strings.ToLower(input.UserInstruction)) {
		if plan.CaptionStyle == nil {
			plan.CaptionStyle = captionStyleForProject(models.ProjectTypeReel, plan.StoryType, plan.VisualStyle)
		}
		plan.CaptionStyle.FontFamily = "inter-bold"
		if plan.CaptionStyle.FontSize < 48 {
			plan.CaptionStyle.FontSize = 48
		}
		plan.CaptionStyle.Position = "bottom"
		plan.CaptionStyle.BottomOffset = maxRevisionInt(plan.CaptionStyle.BottomOffset, 150)
		plan.CaptionStyle.MaxLines = 2
		changes = append(changes, StoryRevisionChange{Type: "caption_style_change", Target: "caption_style", Reason: "improved mobile caption readability"})
	}
	(ReelEditStrategy{}).ApplyPlanDefaults(plan, Request{ProjectType: models.ProjectTypeReel, ProjectConfiguration: input.Story.ProjectConfiguration})
	return changes
}
func (ReelEditStrategy) UnsupportedInstructionWarnings(instruction string) []string {
	return unsupportedInstructionWarnings(instruction)
}

func replaceOpeningIfRequested(plan *models.StoryPlan, input StoryRevisionInput) []StoryRevisionChange {
	instruction := strings.ToLower(input.UserInstruction)
	if !wantsReplaceOpening(instruction) || len(plan.Moments) == 0 {
		return nil
	}
	used := map[string]bool{}
	for _, moment := range plan.Moments {
		used[storyMomentMediaIDForRevision(moment)] = true
	}
	for _, mediaID := range input.Constraints.AllowedMediaIDs {
		mediaID = strings.TrimSpace(mediaID)
		if mediaID == "" || used[mediaID] {
			continue
		}
		replacement, ok := revisionMomentForMedia(mediaID, input)
		if !ok {
			continue
		}
		replacement.MomentID = "moment-opening-" + mediaID
		replacement.Transition = "fade_in"
		old := plan.Moments[0]
		plan.Moments[0] = replacement
		refreshTotalDuration(plan)
		return []StoryRevisionChange{{Type: "replace_opening", MediaID: mediaID, MomentID: replacement.MomentID, Reason: "replaced opening while preserving the rest of the edit"}, {Type: "remove_moment", MediaID: storyMomentMediaIDForRevision(old), MomentID: old.MomentID, Reason: "old opening replaced"}}
	}
	plan.Moments[0].Score += 0.1
	if plan.Moments[0].Score > 1 {
		plan.Moments[0].Score = 1
	}
	plan.Moments[0].Reason = truncateText(strings.TrimSpace(plan.Moments[0].Reason+"; opening strengthened without alternate media"), 240)
	return []StoryRevisionChange{{Type: "opening_refinement", Target: plan.Moments[0].MomentID, Reason: "no unused replacement media was available"}}
}

func wantsReplaceOpening(instruction string) bool {
	return (strings.Contains(instruction, "replace") || strings.Contains(instruction, "change") || strings.Contains(instruction, "new")) &&
		(strings.Contains(instruction, "opening") || strings.Contains(instruction, "hook") || strings.Contains(instruction, "first scene") || strings.Contains(instruction, "scene 1"))
}

func wantsStrongerCTA(instruction string) bool {
	return strings.Contains(instruction, "cta") || strings.Contains(instruction, "call to action") || strings.Contains(instruction, "call-to-action")
}

func wantsCaptionReadability(instruction string) bool {
	return (strings.Contains(instruction, "caption") || strings.Contains(instruction, "subtitle")) &&
		(strings.Contains(instruction, "readable") || strings.Contains(instruction, "clear") || strings.Contains(instruction, "bigger"))
}

func unsupportedInstructionWarnings(instruction string) []string {
	normalized := strings.ToLower(instruction)
	warnings := make([]string, 0)
	if strings.Contains(normalized, "execute") || strings.Contains(normalized, "run ffmpeg") || strings.Contains(normalized, "shell") || strings.Contains(normalized, "sql") || strings.Contains(normalized, "javascript") {
		warnings = append(warnings, "Ignored request for executable code or commands; AI editing only returns structured edit plans.")
	}
	if strings.Contains(normalized, "guarantee viral") || strings.Contains(normalized, "make it viral") {
		warnings = append(warnings, "Cannot guarantee virality; optimized for engagement signals instead.")
	}
	return warnings
}
