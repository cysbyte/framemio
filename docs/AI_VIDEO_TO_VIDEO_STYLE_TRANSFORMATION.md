# AI Video-to-Video Style Transformation Reference

**Status:** Design reference  
**Last reviewed:** September 26, 2026  
**Primary use case:** Change a user's face into a cartoon or illustrated face while preserving the original scene, body motion, camera movement, timing, and audio.

## Problem Definition

The desired transformation is:

```text
original video
  + cartoon face/style reference
  + targeted edit prompt
  -> transformed video
```

The source video must remain available as an immutable original. The transformed video is a new
artifact because its pixels have changed and it must be encoded again.

This is not the same as downloading or copying a source video. A normal copy preserves the original
bytes. AI video transformation reconstructs some or all frames and can introduce visual drift.

## Model Categories

### 1. Full Video-to-Video Editing

The model receives the source video and generates a new version according to an edit instruction.

**Good for:**

- Cartoon or illustrated face transformations
- Re-styling a shot
- Changing clothing, lighting, props, or backgrounds
- Transformations that need synthesized detail

**Risk:**

- Background, clothing, hands, camera framing, or motion can change unintentionally.
- The result is not pixel-identical outside the requested edit.

### 2. Localized Face-Only Editing

The system detects and tracks the face, generates only the face region, then composites it over the
unchanged source frame.

**Good for:**

- “Change only my face”
- Exact preservation of the background and body motion
- Products where scene continuity matters more than generative freedom

**Risk:**

- Requires face detection, tracking, masks, temporal smoothing, and compositing.
- Extreme head turns, occlusion, hair crossing the face, and fast motion need special handling.

### 3. Tracked Compositing

The app applies a prepared cartoon asset or rig to tracked facial landmarks without regenerating
the whole face.

**Good for:**

- Stickers, masks, avatars, and repeatable effects
- Real-time or near-real-time processing
- Strict control over non-face pixels

**Risk:**

- Looks less natural than an AI-generated face.
- Requires a suitable 2D/3D asset and robust tracking.

For FrameMio's “almost identical scene” requirement, localized face-only editing or tracked
compositing should be the default architecture. Full-frame video-to-video should be an optional
creative mode.

## Current Model Fit

Model availability, limits, names, and API access change frequently. Confirm the live provider
documentation before implementation.

### Runway Aleph 2.0

Best fit for an explicit existing-footage edit. Runway describes Aleph 2.0 as an in-context video
editing model: the prompt requests a targeted change and the model attempts to preserve the rest of
the video, including background, lighting, and surrounding details.

- Official prompting guide: <https://help.runwayml.com/hc/en-us/articles/52150503729171-Aleph-2-0-Prompting-Guide>
- Runway product overview: <https://runway.com/product/ai-video-editor>

Use it for a creative full-frame video-to-video mode. Do not treat its preservation language as a
guarantee that non-face pixels remain unchanged.

### Seedance 2.5

Strong fit for instruction-based video editing and reference-driven generation. Its editing guide
describes editing as a “locked” task: the input establishes the timeline, and the prompt should name
the target, requested change, protected properties, and time range.

- Video editing guide: <https://docs.seedance.tv/en/seedance-2-5-video-editing-guide>
- Reference and prompting guide: <https://docs.seedance.tv/en/seedance-2-5-prompting-guide>

Seedance is especially useful when the application needs explicit reference-video and
reference-image roles. It can still regenerate more of the frame than desired.

### PixVerse V6 Omni / Fusion

PixVerse V6 supports video references in Omni mode. Its documentation describes subject
replacement, video recreation, motion imitation, and scene/camera/style reference use cases.

Important documented constraints include:

- Video references require V6 and `reference_mode: "omni"`.
- Up to two reference videos can be supplied.
- The combined reference-video duration cannot exceed 15 seconds.
- With video references, output duration follows the longest reference and `duration` is set to `0`.

- V6 capabilities: <https://docs.platform.pixverse.ai/v6-2056814m0>
- Video reference integration: <https://docs.platform.pixverse.ai/fusion-reference-to-video-v6-video-reference-integration-guide-2269890m0>

Use it as a reference-driven alternative, not as a strict compositor.

### Gemini Omni Flash

Google documents Gemini Omni Flash as a multimodal model for video generation and editing, with
conversational editing through the Interactions API. Runway also exposes a Gemini Omni Flash edit
mode for existing footage.

- Google API documentation: <https://ai.google.dev/gemini-api/docs/omni>
- Runway edit-mode documentation: <https://help.runwayml.com/hc/en-us/articles/53031657806611-Creating-with-Gemini-Omni-Flash>

Verify the exact upload, duration, resolution, and editing limits for the selected access path.
The Runway edit workflow and the Google API workflow are not interchangeable contracts.

## Recommended Product Architecture

```text
User selects source video and cartoon reference
        |
        v
Preserve original upload and metadata
        |
        +--> Inspect duration, dimensions, FPS, audio, orientation
        |
        +--> Detect and track face(s)
        |
        +--> Select processing mode
                 |
                 +--> localized face edit / inpainting
                 +--> full video-to-video model
                 +--> tracked cartoon compositing
        |
        v
Temporal quality checks
        |
        v
Composite or mux original audio
        |
        v
Store source, intermediate, and final artifacts separately
```

### Required Artifacts

Keep these as separate immutable records:

- `source_video`: exact user-uploaded bytes
- `style_reference`: cartoon image, character sheet, or style asset
- `face_tracks`: face boxes, landmarks, confidence, occlusion intervals
- `mask_sequence`: optional per-frame face masks
- `generated_video`: provider output before final assembly
- `final_video`: reviewed/composited output

Never overwrite `source_video` with an AI result.

### Provider Contract

A future provider adapter should accept a provider-neutral request similar to:

```json
{
  "sourceVideo": "artifact://source-video",
  "styleReference": "artifact://cartoon-face",
  "mode": "localized-face-edit",
  "prompt": "Change only the visible face into the supplied cartoon style.",
  "preserve": [
    "camera framing",
    "background",
    "body motion",
    "head pose",
    "lighting",
    "timing",
    "original audio"
  ],
  "startSeconds": 0,
  "endSeconds": 8.0,
  "output": {
    "resolution": "source",
    "frameRate": "source",
    "audio": "original"
  }
}
```

The adapter should return:

```json
{
  "generatedVideo": "artifact://generated-video",
  "provider": "provider-name",
  "model": "model-name",
  "durationSeconds": 8.0,
  "width": 1080,
  "height": 1920,
  "frameRate": 30,
  "audioIncluded": false
}
```

The provider should not silently crop, retime, drop audio, or change aspect ratio. If the selected
model cannot satisfy the requested output, return an explicit capability error.

## Prompt Template

Use a short, targeted prompt. Over-describing the entire scene can encourage the model to recreate
parts that should remain untouched.

```text
Edit only the visible human face in the source video.
Transform that face into the supplied [STYLE] cartoon design.
Preserve the exact face position, head pose, expressions, gaze direction,
original action order, body movement, camera framing, camera motion,
background, clothing, lighting, shadows, depth of field, timing, and scene
composition. Do not add cuts or new objects. Do not alter any pixels outside
the face region unless required for natural edge blending.
Keep the original audio unchanged.
```

For a reference image, map it to one clear role:

```text
Image 1 is the cartoon face/style reference.
Video 1 is the source footage and its motion/timing must be preserved.
```

## Processing Strategy

### Preferred Strict-Preservation Path

1. Upload and hash the original video.
2. Probe the original dimensions, frame rate, duration, rotation, and audio streams.
3. Detect the face and build a temporally stable track.
4. Generate or select a cartoon face for representative frames.
5. Run localized face editing or render a tracked cartoon asset.
6. Composite the generated face onto the original frames.
7. Copy or separately remux the original audio.
8. Compare source and final frames outside the mask.
9. Export the final video without replacing the source artifact.

### Creative Full-Frame Path

1. Split the source into provider-supported segments.
2. Preserve the original segment boundaries and frame rate in metadata.
3. Send each segment with a narrowly scoped prompt and style reference.
4. Validate duration, aspect ratio, frame rate, and visual continuity.
5. Concatenate or assemble outputs only after validation.
6. Restore original audio when the provider does not preserve it.

Short segments generally reduce model drift, but every segment boundary needs overlap or continuity
handling to avoid visible jumps.

## Quality and Validation

Minimum checks before accepting a result:

- Duration remains within an agreed tolerance.
- Width, height, orientation, and frame rate match the requested output.
- No unexpected scene cuts or blank frames.
- Face identity/style remains stable across adjacent frames.
- Face mask does not reveal hard edges or flicker.
- Background and body motion remain stable outside the edited region.
- Original audio remains synchronized.
- Output codec and container are playable on the target clients.

For localized editing, calculate a non-face difference score:

```text
outside_mask_difference =
  changed_pixels(outside_face_mask) / total_pixels(outside_face_mask)
```

Reject or flag results when this exceeds the product threshold. Use perceptual metrics as a review
signal, not as the only acceptance criterion.

## Hypit Integration Notes

The Hypit repository inspected for this design separates source media, model references, and generated
outputs:

- Source files can be downloaded without video re-encoding through `yt-dlp` plus FFmpeg muxing:
  [`packages/yt-dlp/src/download.ts`](/Users/chenyunshu/Downloads/hypit-main/packages/yt-dlp/src/download.ts)
- Video generation models receive explicit `referenceVideo` inputs:
  [`packages/seedance/src/index.ts`](/Users/chenyunshu/Downloads/hypit-main/packages/seedance/src/index.ts)
- HypiHub maps those references to provider request fields such as `reference_videos`:
  [`packages/provider-hypihub/src/mapping.ts`](/Users/chenyunshu/Downloads/hypit-main/packages/provider-hypihub/src/mapping.ts)
- The existing `matte-portrait-video` capability is portrait matting/cutout, not cartoon face
  transformation:
  [`packages/provider-hypihub/src/mapping.ts`](/Users/chenyunshu/Downloads/hypit-main/packages/provider-hypihub/src/mapping.ts)

For a future FrameMio implementation, add a separate provider-neutral capability for
`video-face-style-transform`. Do not overload portrait matting or ordinary media normalization to
represent a generative face edit.

## Safety and Consent

The app should require that the user owns or is authorized to transform the source footage and
faces. Store provenance and consent state with the job. Do not present a generated face result as
an untouched recording, and retain the original source for comparison and recovery.

## Decision Summary

Use **localized face editing or tracked compositing** when the requirement is “change only my face
and keep the scene nearly identical.” Use **Runway Aleph 2.0**, **Seedance 2.5**, **PixVerse V6
Omni**, or **Gemini Omni Flash** when a creative full-frame transformation is acceptable and some
scene drift can be reviewed.

