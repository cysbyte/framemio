import test from 'node:test';
import assert from 'node:assert/strict';
import {hyperframesOverlayRequests} from './renderer.js';
import type {CompositionSpec} from '../types.js';

function composition(overrides: Partial<CompositionSpec> = {}): CompositionSpec {
  return {
    renderer_version: 'framemio-renderer-v1',
    story_plan_id: 'plan-1',
    story_plan_version: 1,
    project_id: 'project-1',
    fps: 30,
    resolution: {width: 1080, height: 1920},
    duration_frames: 150,
    duration_ms: 5000,
    clips: [{
      clip_id: 'clip-1',
      scene_id: 'moment-1',
      asset_id: 'video-1',
      media_type: 'video',
      source_path: '/source/video.mp4',
      prepared_path: '/prepared/video.mp4',
      start_frame: 0,
      duration_frames: 150,
      duration_ms: 5000,
      crop_intent: 'fill',
      transform: {kind: 'video', crop: 'fill', scale: 1},
      transition: 'cut',
      volume: 1,
      speed: 1,
    }],
    broll: [],
    captions: [{moment_id: 'moment-1', text: 'The biggest lesson', start_ms: 1000, end_ms: 2400}],
    audio: {source: 'none', original_audio: false, original_volume: 0},
    ...overrides,
  };
}

test('builds hyperframes requests only for whitelisted caption motion presets', () => {
  assert.deepEqual(hyperframesOverlayRequests(composition({caption_style: {motion_preset: 'raw_css_animation'}})), []);

  const requests = hyperframesOverlayRequests(composition({
    caption_style: {preset: 'bold_social', presentation: 'word_by_word', motion_preset: 'word_pop'},
  }));

  assert.equal(requests.length, 1);
  assert.equal(requests[0].kind, 'caption_overlay');
  assert.equal(requests[0].preset, 'word_pop');
  assert.equal(requests[0].start_ms, 1000);
  assert.equal(requests[0].duration_ms, 1400);
});
