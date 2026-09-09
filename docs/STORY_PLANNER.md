# Story Planner

The Story Planner combines `PhotoAnalysis`, `VideoAnalysis`, `MediaClusters`, timestamps, locations, user-selected media, desired duration, story type, visual style, and optional instructions into a renderer-independent `StoryPlan`.

## Contract

- It never analyzes raw videos.
- It primarily uses `VideoAnalysis` for moment selection.
- It removes repetitive moments and keeps the story chronological where appropriate.
- It assigns title text, ending text, captions when useful, caption style, clip durations, and transitions.
- It honors story edit subtitle preferences by clearing captions when subtitles are disabled and by selecting only approved caption style presets.
- It returns strict JSON-serializable `StoryPlan` data with valid media references.
- External planners pass through a bounded sanitizer before validation. The sanitizer normalizes common renderer-safe aliases for transitions, crop intents, caption styles, caption timing, aspect ratio, and source-audio intent, drops malformed caption rows, and backfills top-level timed captions from transcript cues or moment captions when the external plan omits valid `captions` and subtitles are not disabled. It does not repair invented media or identity fields.
- CloudWatch traces caption repair with `story_planner_external_output_sanitized` at the provider boundary and `story_planner_primary_output_sanitized` before fallback validation. A repaired external plan should then emit `story_planner_primary_succeeded`, not `story_planner_primary_failed_falling_back`.
- Initial external planning strips `moments[].typography`; photo typography is deferred for the first release.

## Validation

The planner validates:

- selected video moments reference a valid media ID;
- selected video moments include a valid `VideoAnalysis` version;
- selected video moments use a positive, increasing source timestamp range;
- captions reference real moments and caption style uses whitelisted renderer-safe values;
- story type, visual style, and output metadata are supported values.
- external planner output that references unknown or excluded media is rejected before persistence and should fall back to the local planner when fallback is configured.

## Output

The resulting plan is intentionally independent of Remotion or any renderer. A later rendering stage can consume the plan without needing to know how the planner scored moments or selected transitions.
