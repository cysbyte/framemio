# Story Planner

The Story Planner combines `PhotoAnalysis`, `VideoAnalysis`, `MediaClusters`, timestamps, locations, user-selected media, desired duration, story type, visual style, and optional instructions into a renderer-independent `StoryPlan`.

## Contract

- It never analyzes raw videos.
- It primarily uses `VideoAnalysis` for moment selection.
- It removes repetitive moments and keeps the story chronological where appropriate.
- It assigns title text, ending text, captions when useful, caption style, clip durations, and transitions.
- It honors story edit subtitle preferences by clearing captions when subtitles are disabled and by selecting only approved caption style presets.
- It returns strict JSON-serializable `StoryPlan` data with valid media references.

## Validation

The planner validates:

- selected video moments reference a valid media ID;
- selected video moments include a valid `VideoAnalysis` version;
- selected video moments use a positive, increasing source timestamp range;
- captions reference real moments and caption style uses whitelisted renderer-safe values;
- story type, visual style, and output metadata are supported values.

## Output

The resulting plan is intentionally independent of Remotion or any renderer. A later rendering stage can consume the plan without needing to know how the planner scored moments or selected transitions.
