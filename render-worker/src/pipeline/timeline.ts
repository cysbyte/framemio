import path from 'node:path';
import {
  DEFAULT_FPS,
  deterministicUnit,
  framesFromMilliseconds,
  millisecondsFromFrames,
  resolutionForAspectRatio,
  stableClipId,
} from '../determinism.js';
import type {
  CompositionSpec,
  KenBurnsTransform,
  RenderPaths,
  ResolvedAudio,
  SourceMediaAsset,
  StoryMoment,
  StoryPlan,
  TimelineClip,
  TransitionType,
  VideoTransform,
} from '../types.js';
import {RENDER_VERSION} from '../types.js';
import {RenderInputError} from './errors.js';

const SUPPORTED_TRANSITIONS = new Set(['cut', 'fade', 'dissolve']);

export interface TimelineOptions {
  rendererVersion?: string;
  fps?: number;
}

export function buildComposition(
  storyPlan: StoryPlan,
  mediaAssets: SourceMediaAsset[],
  assetPaths: Map<string, string>,
  preparedPaths: Map<string, string>,
  paths: RenderPaths,
  options: TimelineOptions = {},
): CompositionSpec {
  const fps = options.fps ?? DEFAULT_FPS;
  const rendererVersion = options.rendererVersion ?? RENDER_VERSION;
  const resolution = resolutionForAspectRatio(storyPlan.output.aspect_ratio);
  const assetsByID = new Map(mediaAssets.map((asset) => [asset.id, asset]));
  let nextFrame = 0;
  const clips: TimelineClip[] = storyPlan.moments.map((moment, index) => {
    const asset = assetsByID.get(moment.asset_id);
    if (!asset) {
      throw new RenderInputError(`missing media asset: ${moment.asset_id}`);
    }
    const durationFrames = framesFromMilliseconds(moment.duration_ms, fps);
    const clipID = stableClipId(moment, index);
    const sourcePath = assetPaths.get(asset.id);
    const preparedPath = preparedPaths.get(clipID) ?? sourcePath;
    if (!sourcePath || !preparedPath) {
      throw new RenderInputError(`missing prepared path for asset: ${asset.id}`);
    }
    const clip: TimelineClip = {
      clip_id: clipID,
      asset_id: asset.id,
      media_type: asset.media_type,
      source_path: sourcePath,
      prepared_path: preparedPath,
      start_frame: nextFrame,
      duration_frames: durationFrames,
      duration_ms: millisecondsFromFrames(durationFrames, fps),
      source_start_micros: moment.start_micros,
      source_end_micros: moment.end_micros,
      crop_intent: moment.crop_intent ?? 'fill',
      transform: asset.media_type === 'video' ? videoTransform(moment) : photoTransform(moment, clipID),
      transition: transitionFor(moment),
      volume: clamp(moment.volume ?? 1, 0, 1),
      speed: speedFor(moment),
    };
    nextFrame += durationFrames;
    return clip;
  });
  return {
    renderer_version: rendererVersion,
    story_plan_id: storyPlan.id,
    story_plan_version: storyPlan.version,
    project_id: storyPlan.project_id,
    fps,
    resolution,
    duration_frames: nextFrame,
    duration_ms: millisecondsFromFrames(nextFrame, fps),
    title_text: storyPlan.title_text,
    ending_text: storyPlan.ending_text,
    clips,
    captions: storyPlan.captions ?? [],
    audio: resolveAudio(storyPlan, paths, fps),
  };
}

export function mediaExtension(asset: SourceMediaAsset): string {
  if (asset.file_name && path.extname(asset.file_name)) {
    return path.extname(asset.file_name);
  }
  if (asset.media_type === 'video') {
    return '.mov';
  }
  return '.jpg';
}

function photoTransform(moment: StoryMoment, seed: string): KenBurnsTransform {
  const intent = moment.crop_intent ?? 'zoom';
  const panX = round((deterministicUnit(seed, 'pan-x') - 0.5) * 0.08, 4);
  const panY = round((deterministicUnit(seed, 'pan-y') - 0.5) * 0.08, 4);
  const zoomIn = intent !== 'pan' && deterministicUnit(seed, 'zoom-direction') >= 0.5;
  return {
    kind: 'photo',
    from_scale: zoomIn ? 1.03 : 1.1,
    to_scale: zoomIn ? 1.1 : 1.03,
    from_x: round(-panX, 4),
    from_y: round(-panY, 4),
    to_x: panX,
    to_y: panY,
  };
}

function videoTransform(moment: StoryMoment): VideoTransform {
  return {
    kind: 'video',
    crop: moment.crop_intent === 'fit' ? 'fit' : 'fill',
    scale: 1,
  };
}

function transitionFor(moment: StoryMoment): TransitionType {
  const transition = String(moment.transition ?? 'cut').toLowerCase();
  return SUPPORTED_TRANSITIONS.has(transition) ? (transition as TransitionType) : 'cut';
}

function speedFor(moment: StoryMoment): number {
  return clamp(moment.speed ?? 1, 0.25, 4);
}

function resolveAudio(storyPlan: StoryPlan, paths: RenderPaths, fps: number): ResolvedAudio {
  const audio = storyPlan.audio;
  if (!audio || audio.source === 'none') {
    return {source: 'none', original_audio: false, background_volume: 0, original_volume: 0, ducking: false, fade_in_frames: 0, fade_out_frames: 0};
  }
  return {
    source: audio.source,
    mood: audio.mood,
    background_path: audio.background_s3_key ? path.join(paths.assets_dir, 'background-audio') : undefined,
    original_audio: audio.original_audio ?? true,
    background_volume: clamp(audio.background_volume ?? 0.36, 0, 1),
    original_volume: clamp(audio.original_volume ?? 1, 0, 1),
    ducking: audio.ducking ?? true,
    fade_in_frames: framesFromMilliseconds(audio.fade_in_ms ?? 800, fps),
    fade_out_frames: framesFromMilliseconds(audio.fade_out_ms ?? 1200, fps),
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function round(value: number, digits: number): number {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}
