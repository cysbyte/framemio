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
  BrandingSpec,
  CompositionEffect,
  CompositionMediaAsset,
  CompositionSpec,
  CompositionTransition,
  CTASpec,
  KenBurnsTransform,
  RenderPaths,
  ResolvedAudio,
  ProjectType,
  SourceMediaAsset,
  StoryBRoll,
  StoryMoment,
  StoryPlan,
  TimelineBRoll,
  TimelineClip,
  TimelineScene,
  TextOverlay,
  TransitionType,
  VideoTransform,
} from '../types.js';
import {RENDER_VERSION} from '../types.js';
import {RenderInputError} from './errors.js';

const SUPPORTED_TRANSITIONS = new Set(['cut', 'fade', 'dissolve', 'crossfade']);

export interface TimelineOptions {
  rendererVersion?: string;
  fps?: number;
  projectType?: ProjectType;
  projectConfiguration?: Record<string, unknown>;
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
  const projectType = normalizeProjectType(options.projectType ?? storyPlan.project_type);
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
      scene_id: moment.moment_id ?? clipID,
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
      overlays: normalizeSceneOverlays(moment, moment.duration_ms),
      typography: normalizeTypography(moment.typography, moment.duration_ms),
      order: normalizePositiveInt(moment.order),
      scene_purpose: normalizeScenePurpose(moment.scene_purpose),
      scene_text: normalizeSceneText(moment.scene_text),
      voiceover: normalizeVoiceover(moment.voiceover, moment.duration_ms),
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
  const media = compositionMediaAssets(mediaAssets, assetPaths, preparedPaths, clips, broll);
  const scenes = normalizedScenes(clips, media);
  const text = normalizedCompositionText(clips);
  const transitions = normalizedTransitions(clips, fps);
  const effects = normalizedEffects(clips);
  const branding = normalizeBranding(options.projectConfiguration, mediaAssets, assetPaths);
  const cta = normalizeCTA(storyPlan.call_to_action);
  return {
    renderer_version: rendererVersion,
    project_type: projectType,
    project_configuration: options.projectConfiguration,
    story_plan_id: storyPlan.id,
    story_plan_version: storyPlan.version,
    project_id: storyPlan.project_id,
    canvas: {
      width: resolution.width,
      height: resolution.height,
      aspect_ratio: storyPlan.output.aspect_ratio,
      fps,
      background_color: '#0f0f12',
      safe_area: projectType === 'reel' ? {top: 0.08, right: 0.08, bottom: 0.16, left: 0.08} : {top: 0.06, right: 0.06, bottom: 0.08, left: 0.06},
    },
    fps,
    resolution,
    duration_frames: nextFrame,
    duration_ms: millisecondsFromFrames(nextFrame, fps),
    duration: {frames: nextFrame, ms: millisecondsFromFrames(nextFrame, fps)},
    title_text: storyPlan.title_text,
    ending_text: storyPlan.ending_text,
    call_to_action: storyPlan.call_to_action,
    cta,
    branding,
    media,
    scenes,
    text,
    transitions,
    effects,
    clips,
    broll,
    captions: storyPlan.captions ?? [],
    caption_style: storyPlan.caption_style,
    audio: resolveAudio(storyPlan, paths, fps),
  };
}

function normalizeSceneOverlays(moment: StoryMoment, momentDurationMs: number): TextOverlay[] {
  const overlays: TextOverlay[] = [];
  const typography = normalizeTypography(moment.typography, momentDurationMs);
  if (typography) {
    overlays.push(typography);
  }
  const sceneText = normalizeSceneText(moment.scene_text);
  if (sceneText) {
    const title = sceneText.headline || sceneText.cta;
    const subtitle = sceneText.supporting;
    if (title || subtitle) {
      const overlay = normalizeTypography({
        enabled: true,
        type: sceneText.cta && !sceneText.headline ? 'promo_cta' : 'promo_headline',
        title,
        subtitle,
        style: 'editorial',
        placement: sceneText.cta && !sceneText.headline ? 'bottom_center' : 'bottom_left',
        start_ms: 250,
        duration_ms: Math.max(250, Math.min(momentDurationMs - 250, 2200)),
        max_width: 0.78,
        safe_area: {top: 0.08, right: 0.08, bottom: 0.1, left: 0.08},
        animation: 'fade_up',
        source: 'ai',
      }, momentDurationMs);
      if (overlay) {
        overlays.push(overlay);
      }
    }
  }
  return overlays;
}

function compositionMediaAssets(
  assets: SourceMediaAsset[],
  assetPaths: Map<string, string>,
  preparedPaths: Map<string, string>,
  clips: TimelineClip[],
  broll: TimelineBRoll[],
): CompositionMediaAsset[] {
  const preparedByAsset = new Map<string, string>();
  for (const clip of [...clips, ...broll]) {
    preparedByAsset.set(clip.asset_id, clip.prepared_path);
  }
  return assets
    .filter((asset) => assetPaths.has(asset.id))
    .map((asset) => ({
      asset_id: asset.id,
      media_type: asset.media_type,
      source_path: assetPaths.get(asset.id) ?? '',
      prepared_path: preparedByAsset.get(asset.id),
      width: asset.width,
      height: asset.height,
      duration_seconds: asset.duration_seconds,
    }));
}

function normalizedScenes(clips: TimelineClip[], media: CompositionMediaAsset[]): TimelineScene[] {
  const mediaByID = new Map(media.map((asset) => [asset.asset_id, asset]));
  return clips.map((clip) => ({
    ...clip,
    id: clip.scene_id ?? clip.clip_id,
    media: mediaByID.get(clip.asset_id) ?? {asset_id: clip.asset_id, media_type: clip.media_type, source_path: clip.source_path, prepared_path: clip.prepared_path},
  }));
}

function normalizedCompositionText(clips: TimelineClip[]): TextOverlay[] {
  return clips.flatMap((clip) => clip.overlays ?? []);
}

function normalizedTransitions(clips: TimelineClip[], fps: number): CompositionTransition[] {
  return clips.map((clip) => ({
    scene_id: clip.scene_id ?? clip.clip_id,
    type: clip.transition,
    duration_frames: Math.max(1, Math.round(Math.min(0.45, Math.max(0.001, clip.duration_ms / 3000)) * fps)),
  }));
}

function normalizedEffects(clips: TimelineClip[]): CompositionEffect[] {
  return clips.map((clip) => ({scene_id: clip.scene_id ?? clip.clip_id, type: clip.transform.kind === 'photo' ? 'ken_burns' : `video_${clip.transform.crop}`, parameters: {...clip.transform} as Record<string, unknown>}));
}

function normalizeCTA(value?: string): CTASpec | undefined {
  const text = cleanTypographyText(value).slice(0, 80);
  return text ? {text, placement: 'bottom_center', style: 'editorial'} : undefined;
}

function normalizeBranding(configuration: Record<string, unknown> | undefined, assets: SourceMediaAsset[], assetPaths: Map<string, string>): BrandingSpec | undefined {
  const promo = typeof configuration?.promo === 'object' && configuration?.promo !== null ? configuration.promo as Record<string, unknown> : undefined;
  const nestedBrand = typeof promo?.brand === 'object' && promo?.brand !== null ? promo.brand as Record<string, unknown> : undefined;
  const topLevelBrand = typeof configuration?.brand === 'object' && configuration?.brand !== null ? configuration.brand as Record<string, unknown> : undefined;
  const brand = nestedBrand ?? topLevelBrand;
  if (!brand) {
    return undefined;
  }
  const logoAssetID = typeof brand.logo_asset_id === 'string' ? brand.logo_asset_id : undefined;
  const logoAsset = logoAssetID ? assets.find((asset) => asset.id === logoAssetID) : undefined;
  return {
    name: typeof brand.name === 'string' ? cleanTypographyText(brand.name).slice(0, 80) : undefined,
    primary_color: typeof brand.primary_color === 'string' ? brand.primary_color : undefined,
    secondary_color: typeof brand.secondary_color === 'string' ? brand.secondary_color : undefined,
    logo_asset_id: logoAssetID,
    logo_path: logoAsset ? assetPaths.get(logoAsset.id) : undefined,
    font_preference: typeof brand.font_preference === 'string' ? cleanTypographyText(brand.font_preference).slice(0, 80) : undefined,
  };
}

function normalizeProjectType(value?: ProjectType): ProjectType {
  return value === 'promo' || value === 'reel' ? value : 'story';
}

function normalizeTypography(typography: StoryMoment['typography'], momentDurationMs: number): TextOverlay | undefined {
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

function normalizePositiveInt(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 ? value : undefined;
}

function normalizeScenePurpose(value: unknown): StoryMoment['scene_purpose'] | undefined {
  return typeof value === 'string' && ['hook', 'problem', 'product', 'feature', 'benefit', 'proof', 'testimonial', 'cta', 'outro'].includes(value) ? value : undefined;
}

function normalizeSceneText(value: StoryMoment['scene_text']): StoryMoment['scene_text'] | undefined {
  if (!value) {
    return undefined;
  }
  const headline = cleanTypographyText(value.headline).slice(0, 80);
  const supporting = cleanTypographyText(value.supporting).slice(0, 80);
  const cta = cleanTypographyText(value.cta).slice(0, 80);
  return headline || supporting || cta ? {headline, supporting, cta} : undefined;
}

function normalizeVoiceover(value: StoryMoment['voiceover'], momentDurationMs: number): StoryMoment['voiceover'] | undefined {
  if (!value) {
    return undefined;
  }
  const text = cleanTypographyText(value.text).slice(0, 220);
  if (!text) {
    return undefined;
  }
  const startMs = clamp(Math.round(value.start_ms ?? 0), 0, Math.max(0, momentDurationMs));
  const durationMs = clamp(Math.round(value.duration_ms ?? momentDurationMs - startMs), 0, Math.max(0, momentDurationMs - startMs));
  return {text, start_ms: startMs, duration_ms: durationMs};
}

function cleanTypographyText(value: unknown): string {
  return typeof value === 'string' ? value.replace(/[\u0000-\u001f\u007f]/g, ' ').replace(/\s+/g, ' ').trim() : '';
}

function supportedTypographyType(value: unknown): string {
  return typeof value === 'string' && ['moment_title', 'moment_caption', 'location', 'date', 'chapter_title', 'story_intro', 'story_outro', 'promo_headline', 'promo_support', 'promo_cta'].includes(value) ? value : 'moment_title';
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
