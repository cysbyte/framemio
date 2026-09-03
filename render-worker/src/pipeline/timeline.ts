import path from 'node:path';
import {
  DEFAULT_FPS,
  deterministicUnit,
  framesFromMilliseconds,
  millisecondsFromFrames,
  resolutionForAspectRatio,
  stableBRollClipId,
  stableClipId,
} from '../determinism.js';
import type {
  CompositionSpec,
  KenBurnsTransform,
  RenderPaths,
  ResolvedAudio,
  SourceMediaAsset,
  StoryBRoll,
  StoryMoment,
  StoryPlan,
  TimelineBRoll,
  TimelineClip,
  TransitionType,
  VideoTransform,
} from '../types.js';
import {RENDER_VERSION} from '../types.js';
import {RenderInputError} from './errors.js';

const SUPPORTED_TRANSITIONS = new Set(['cut', 'fade', 'dissolve', 'crossfade']);

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
      typography: normalizeTypography(moment.typography, moment.duration_ms),
    };
    nextFrame += durationFrames;
    return clip;
  });
  const broll: TimelineBRoll[] = (storyPlan.broll ?? []).map((overlay, index) => {
    const asset = assetsByID.get(overlay.asset_id);
    if (!asset) {
      throw new RenderInputError(`missing b-roll media asset: ${overlay.asset_id}`);
    }
    const overlayID = stableBRollClipId(overlay, index);
    const sourcePath = assetPaths.get(asset.id);
    const preparedPath = preparedPaths.get(overlayID) ?? sourcePath;
    if (!sourcePath || !preparedPath) {
      throw new RenderInputError(`missing prepared path for b-roll asset: ${asset.id}`);
    }
    const durationFrames = framesFromMilliseconds(overlay.duration_ms, fps);
    return {
      broll_id: overlay.broll_id ?? overlayID,
      asset_id: asset.id,
      media_type: asset.media_type,
      source_path: sourcePath,
      prepared_path: preparedPath,
      start_frame: Math.max(0, Math.round((overlay.start_ms / 1000) * fps)),
      duration_frames: durationFrames,
      duration_ms: millisecondsFromFrames(durationFrames, fps),
      source_start_micros: overlay.source_start_micros,
      source_end_micros: overlay.source_end_micros,
      crop_intent: overlay.crop_intent ?? 'fill',
      transform: asset.media_type === 'video' ? brollVideoTransform(overlay) : brollPhotoTransform(overlay, overlayID),
    };
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
    broll,
    captions: storyPlan.captions ?? [],
    caption_style: storyPlan.caption_style,
    audio: resolveAudio(storyPlan, paths, fps),
  };
}

function normalizeTypography(typography: StoryMoment['typography'], momentDurationMs: number): StoryMoment['typography'] | undefined {
  if (!typography?.enabled) {
    return undefined;
  }
  const title = cleanTypographyText(typography.title).slice(0, 40);
  const subtitle = cleanTypographyText(typography.subtitle).slice(0, 100);
  if (!title && !subtitle) {
    return undefined;
  }
  const startMs = clamp(Math.round(typography.start_ms ?? 350), 0, Math.max(0, momentDurationMs - 250));
  const durationMs = clamp(Math.round(typography.duration_ms ?? momentDurationMs - startMs - 250), 250, Math.max(250, momentDurationMs - startMs));
  return {
    enabled: true,
    type: supportedTypographyType(typography.type),
    title,
    subtitle,
    style: supportedTypographyStyle(typography.style),
    placement: supportedTextPlacement(typography.placement),
    start_ms: startMs,
    duration_ms: durationMs,
    max_width: clampNumber(typography.max_width ?? 0.72, 0.2, 0.95),
    safe_area: {
      top: clampNumber(typography.safe_area?.top ?? 0.08, 0, 0.5),
      right: clampNumber(typography.safe_area?.right ?? 0.08, 0, 0.5),
      bottom: clampNumber(typography.safe_area?.bottom ?? 0.08, 0, 0.5),
      left: clampNumber(typography.safe_area?.left ?? 0.08, 0, 0.5),
    },
    animation: supportedTextAnimation(typography.animation),
    source: typography.source === 'user' || typography.source === 'system' ? typography.source : 'ai',
    confidence: clampNumber(typography.confidence ?? 0, 0, 1),
  };
}

function cleanTypographyText(value: unknown): string {
  return typeof value === 'string' ? value.replace(/[\u0000-\u001f\u007f]/g, ' ').replace(/\s+/g, ' ').trim() : '';
}

function supportedTypographyType(value: unknown): string {
  return typeof value === 'string' && ['moment_title', 'moment_caption', 'location', 'date', 'chapter_title', 'story_intro', 'story_outro'].includes(value) ? value : 'moment_title';
}

function supportedTypographyStyle(value: unknown): string {
  return typeof value === 'string' && ['cinematic', 'minimal', 'editorial', 'memory', 'location'].includes(value) ? value : 'cinematic';
}

function supportedTextPlacement(value: unknown): string {
  return typeof value === 'string' && ['top_left', 'top_center', 'top_right', 'center_left', 'center', 'center_right', 'bottom_left', 'bottom_center', 'bottom_right'].includes(value) ? value : 'bottom_left';
}

function supportedTextAnimation(value: unknown): string {
  return typeof value === 'string' && ['fade', 'fade_up', 'fade_down', 'subtle_scale', 'none'].includes(value) ? value : 'fade_up';
}

function clampNumber(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, Number.isFinite(value) ? value : min));
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

function brollPhotoTransform(overlay: StoryBRoll, seed: string): KenBurnsTransform {
  return photoTransform({asset_id: overlay.asset_id, duration_ms: overlay.duration_ms, crop_intent: overlay.crop_intent}, seed);
}

function brollVideoTransform(overlay: StoryBRoll): VideoTransform {
  return {kind: 'video', crop: overlay.crop_intent === 'fit' ? 'fit' : 'fill', scale: 1};
}

function transitionFor(moment: StoryMoment): TransitionType {
  const transition = String(moment.transition ?? 'cut').toLowerCase();
  if (transition === 'soft_cut') {
    return 'cut';
  }
  if (transition === 'fade_in') {
    return 'fade';
  }
  return SUPPORTED_TRANSITIONS.has(transition) ? (transition as TransitionType) : 'cut';
}

function speedFor(moment: StoryMoment): number {
  return clamp(moment.speed ?? 1, 0.25, 4);
}

function resolveAudio(storyPlan: StoryPlan, _paths: RenderPaths, _fps: number): ResolvedAudio {
  const audio = storyPlan.audio;
  if (audio?.source === 'none' || audio?.original_audio === false) {
    return {source: 'none', original_audio: false, original_volume: 0};
  }
  return {
    source: audio?.source ?? 'original',
    original_audio: true,
    original_volume: clamp(audio?.original_volume ?? 1, 0, 1),
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function round(value: number, digits: number): number {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}
