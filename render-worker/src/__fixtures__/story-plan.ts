import type {RenderInput, SourceMediaAsset, StoryPlan} from '../types.js';

export function fixtureMediaAssets(): SourceMediaAsset[] {
  return [
    {
      id: 'photo-1',
      media_type: 'photo',
      s3_key: 'source/photo-1.jpg',
      content_type: 'image/jpeg',
      width: 4032,
      height: 3024,
      file_name: 'IMG_1001.jpg',
      etag: 'photo-etag',
    },
    {
      id: 'video-1',
      media_type: 'video',
      s3_key: 'source/video-1.mov',
      content_type: 'video/quicktime',
      duration_seconds: 12.4,
      width: 3840,
      height: 2160,
      file_name: 'IMG_1002.MOV',
      etag: 'video-etag',
    },
  ];
}

export function fixtureStoryPlan(overrides: Partial<StoryPlan> = {}): StoryPlan {
  return {
    schema_version: 'story_plan_v1',
    id: 'story-plan-1',
    project_id: 'project-1',
    version: 3,
    requested_duration_ms: 9000,
    title_text: 'A Small Weekend',
    ending_text: 'Made from your moments',
    moments: [
      {
        moment_id: 'moment-photo-1',
        asset_id: 'photo-1',
        media_type: 'photo',
        duration_ms: 3000,
        crop_intent: 'zoom',
        transition: 'sparkle',
      },
      {
        moment_id: 'moment-video-1',
        asset_id: 'video-1',
        media_type: 'video',
        start_micros: 1_250_000,
        end_micros: 5_750_000,
        duration_ms: 4500,
        crop_intent: 'fill',
        transition: 'fade',
        speed: 1.25,
        volume: 0.72,
      },
    ],
    captions: [{moment_id: 'moment-video-1', text: 'Golden hour', start_ms: 3200, end_ms: 4700}],
    audio: {
      source: 'licensed',
      mood: 'warm',
      background_s3_key: 'music/warm-theme.m4a',
      original_audio: true,
      background_volume: 0.3,
      original_volume: 0.9,
      ducking: true,
      fade_in_ms: 600,
      fade_out_ms: 900,
    },
    output: {aspect_ratio: '9:16', max_duration_seconds: 15, preset: 'social'},
    total_duration_ms: 7500,
    created_at: '2026-08-21T00:00:00.000Z',
    ...overrides,
  };
}

export function fixtureRenderInput(overrides: Partial<RenderInput> = {}): RenderInput {
  return {
    job_id: 'render-job-1',
    project_id: 'project-1',
    story_plan: fixtureStoryPlan(),
    media_assets: fixtureMediaAssets(),
    output_s3_key: 'renders/project-1/story.mp4',
    ...overrides,
  };
}
