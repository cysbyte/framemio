import {spawn} from 'node:child_process';
import {copyFile, mkdir, open, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import type {CaptionStyle, CompositionSpec, KenBurnsTransform, StoryCaption, TextOverlay, TimelineBRoll, TimelineClip} from '../types.js';
import {canonicalJson, stableHash} from '../determinism.js';

export interface RemotionRenderer {
  renderComposition(composition: CompositionSpec, outputPath: string): Promise<void>;
}

export class FixtureRemotionRenderer implements RemotionRenderer {
  readonly rendered: CompositionSpec[] = [];

  async renderComposition(composition: CompositionSpec, outputPath: string): Promise<void> {
    this.rendered.push(composition);
    await mkdir(path.dirname(outputPath), {recursive: true});
    await writeFile(outputPath, canonicalJson(composition), 'utf8');
  }
}

export interface RemotionCliRendererOptions {
  entryPoint: string;
  compositionID?: string;
  remotionBinary?: string;
  timeoutMs?: number;
}

export class RemotionCliRenderer implements RemotionRenderer {
  private readonly compositionID: string;
  private readonly remotionBinary: string;
  private readonly timeoutMs: number;

  constructor(private readonly options: RemotionCliRendererOptions) {
    this.compositionID = options.compositionID ?? 'FramemioStory';
    this.remotionBinary = options.remotionBinary ?? 'npx';
    this.timeoutMs = options.timeoutMs ?? 15 * 60 * 1000;
  }

  async renderComposition(composition: CompositionSpec, outputPath: string): Promise<void> {
    await mkdir(path.dirname(outputPath), {recursive: true});
    const hash = stableHash(composition).slice(0, 20);
    const publicDir = path.join(tmpdir(), `framemio-remotion-public-${hash}`);
    const remotionComposition = await prepareRemotionStaticAssets(composition, publicDir);
    const propsPath = path.join(tmpdir(), `framemio-remotion-${hash}.json`);
    await writeFile(propsPath, canonicalJson({composition: remotionComposition}), 'utf8');
    const args = this.remotionBinary === 'npx'
      ? ['remotion', 'render', this.options.entryPoint, this.compositionID, outputPath]
      : ['render', this.options.entryPoint, this.compositionID, outputPath];
    args.push('--props', propsPath, '--codec', 'h264', '--overwrite', '--concurrency', '1', '--public-dir', publicDir);
    await run(this.remotionBinary, args, this.timeoutMs);
  }
}

async function prepareRemotionStaticAssets(composition: CompositionSpec, publicDir: string): Promise<CompositionSpec> {
  const copied = new Map<string, string>();
  const toStaticAsset = async (sourcePath: string): Promise<string> => {
    if (!sourcePath || /^[a-z]+:\/\//i.test(sourcePath) || sourcePath.startsWith('remotion-static:')) {
      return sourcePath;
    }
    const existing = copied.get(sourcePath);
    if (existing) {
      return existing;
    }
    await mkdir(publicDir, {recursive: true});
    const extension = path.extname(sourcePath) || '.bin';
    const fileName = `asset-${copied.size}-${stableHash(sourcePath).slice(0, 16)}${extension}`;
    await copyFile(sourcePath, path.join(publicDir, fileName));
    const reference = `remotion-static:${fileName}`;
    copied.set(sourcePath, reference);
    return reference;
  };

  return {
    ...composition,
    clips: await Promise.all(composition.clips.map(async (clip) => ({...clip, prepared_path: await toStaticAsset(clip.prepared_path)}))),
    scenes: composition.scenes ? await Promise.all(composition.scenes.map(async (scene) => ({
      ...scene,
      prepared_path: await toStaticAsset(scene.prepared_path),
      media: {...scene.media, source_path: await toStaticAsset(scene.media.source_path), prepared_path: scene.media.prepared_path ? await toStaticAsset(scene.media.prepared_path) : undefined},
    }))) : undefined,
    media: composition.media ? await Promise.all(composition.media.map(async (asset) => ({...asset, source_path: await toStaticAsset(asset.source_path), prepared_path: asset.prepared_path ? await toStaticAsset(asset.prepared_path) : undefined}))) : undefined,
    branding: composition.branding?.logo_path ? {...composition.branding, logo_path: await toStaticAsset(composition.branding.logo_path)} : composition.branding,
    broll: await Promise.all(composition.broll.map(async (overlay) => ({...overlay, prepared_path: await toStaticAsset(overlay.prepared_path)}))),
  };
}

type PhotoTimelineClip = TimelineClip & {transform: KenBurnsTransform};
type PhotoTimelineBRoll = TimelineBRoll & {transform: KenBurnsTransform};

interface ConcatFilter {
  filter: string;
  hasAudio: boolean;
}

interface CaptionOverlay {
  text: string;
  textFilePath: string;
  startSeconds: string;
  endSeconds: string;
  sequenceStartSeconds: string;
  sequenceEndSeconds: string;
  motionPreset: 'none' | 'word_pop' | 'karaoke_pop' | 'phrase_punch';
}

interface TypographyOverlay {
  textFilePath: string;
  startSeconds: string;
  endSeconds: string;
  placement: string;
  style: string;
  kind: 'title' | 'subtitle';
  safeArea: Required<NonNullable<TextOverlay['safe_area']>>;
  maxWidth: number;
  lineCount: number;
  groupHeight: number;
  offsetY: number;
  animation: string;
}

interface ResolvedCaptionStyle {
  fontFamily?: string;
  fontSize: number;
  textColor: string;
  highlightColor: string;
  presentation: 'full_text' | 'word_by_word';
  motionPreset: 'none' | 'word_pop' | 'karaoke_pop' | 'phrase_punch';
  strokeColor: string;
  strokeWidth: number;
  backgroundEnabled: boolean;
  backgroundColor: string;
  backgroundOpacity: number;
  position: 'bottom' | 'middle' | 'top';
  bottomOffset: number;
  maxLines: number;
  maxCharactersPerLine: number;
}

interface ResolvedTypographyStyle {
  fontFamily: string;
  titleFontSize: number;
  subtitleFontSize: number;
  textColor: string;
  subtitleColor: string;
  shadowColor: string;
  shadowOpacity: number;
  strokeWidth: number;
  backgroundEnabled: boolean;
  backgroundColor: string;
  backgroundOpacity: number;
}

const DEFAULT_CAPTION_STYLE: ResolvedCaptionStyle = {
  fontSize: 44,
  textColor: 'white',
  highlightColor: '0xFFD60A',
  presentation: 'full_text',
  motionPreset: 'none',
  strokeColor: 'black',
  strokeWidth: 3,
  backgroundEnabled: true,
  backgroundColor: 'black',
  backgroundOpacity: 0.32,
  position: 'bottom',
  bottomOffset: 150,
  maxLines: 3,
  maxCharactersPerLine: 32,
};

const FONT_FAMILY_MAP = new Map<string, string>([
  // Keep the public font identifiers stable while using the bundled Noto
  // Sans family for consistent FFmpeg output across render environments.
  ['inter', 'Noto Sans'],
  ['inter-bold', 'Noto Sans Bold'],
  ['system', 'Noto Sans'],
  ['system-bold', 'Noto Sans Bold'],
  ['noto-sans', 'Noto Sans'],
  ['noto-sans-bold', 'Noto Sans Bold'],
]);

const TYPOGRAPHY_FONT_FAMILY_MAP = new Map<string, string>([
  ['cinematic', 'Liberation Serif'],
  ['minimal', 'Liberation Sans'],
  ['editorial', 'Liberation Serif'],
  ['memory', 'Liberation Sans'],
  ['location', 'Liberation Sans'],
]);

const DEFAULT_TYPOGRAPHY_STYLE: ResolvedTypographyStyle = {
  fontFamily: 'Liberation Sans',
  titleFontSize: 62,
  subtitleFontSize: 34,
  textColor: 'white',
  subtitleColor: 'white',
  shadowColor: 'black',
  shadowOpacity: 0.54,
  strokeWidth: 1,
  backgroundEnabled: false,
  backgroundColor: 'black',
  backgroundOpacity: 0.22,
};

const SCENE_FADE_SECONDS = 0.45;

interface ClipTransitionEffects {
  fadeInSeconds: number;
  fadeOutSeconds: number;
}

function concatFilter(composition: CompositionSpec, captions: CaptionOverlay[] = [], typography: TypographyOverlay[] = []): ConcatFilter {
  const videoFilters = composition.clips.map((clip, index) => isPhotoTimelineClip(clip)
    ? photoVideoFilter(clip, index, composition, transitionEffects(composition.clips, index))
    : sourceVideoFilter(clip, index, composition, transitionEffects(composition.clips, index)));
  const videoInputs = composition.clips.map((_, index) => `[v${index}]`).join('');
  const filters = [...videoFilters, `${videoInputs}concat=n=${composition.clips.length}:v=1:a=0,format=yuv420p[basev]`];
  const brollFilters = brollVideoFilters(composition);
  filters.push(...brollFilters.filters);
  const typographyFilters = typographyVideoFilter(typography, brollFilters.outputLabel);
  if (typographyFilters.filter) {
    filters.push(typographyFilters.filter);
  }
  filters.push(captionVideoFilter(captions, composition.caption_style, typographyFilters.outputLabel));

  const audioOutputs: string[] = [];
  if (composition.audio.original_audio) {
    const audioFilters = composition.clips.map((clip, index) => isPhotoTimelineClip(clip)
      ? silentAudioFilter(clip, index, transitionEffects(composition.clips, index))
      : sourceAudioFilter(clip, index, composition, transitionEffects(composition.clips, index)));
    const audioInputs = composition.clips.map((_, index) => `[a${index}]`).join('');
    filters.push(...audioFilters, `${audioInputs}concat=n=${composition.clips.length}:v=0:a=1[originala]`);
    audioOutputs.push('[originala]');
  }

  if (audioOutputs.length === 0) {
    return {filter: filters.join(';'), hasAudio: false};
  }
  if (audioOutputs.length === 1) {
    filters.push(`${audioOutputs[0]}anull[outa]`);
  } else {
    filters.push(`${audioOutputs.join('')}amix=inputs=${audioOutputs.length}:duration=first:normalize=0[outa]`);
  }
  return {filter: filters.join(';'), hasAudio: true};
}

function typographyVideoFilter(overlays: TypographyOverlay[], inputLabel = 'basev'): {filter: string; outputLabel: string} {
  if (overlays.length === 0) {
    return {filter: '', outputLabel: inputLabel};
  }
  const filters: string[] = [];
  let input = inputLabel;
  overlays.forEach((overlay, index) => {
    const output = index === overlays.length - 1 ? 'typographyv' : `typographyv${index}`;
    filters.push(`[${input}]drawtext=${typographyDrawtextOptions(overlay)},format=yuv420p[${output}]`);
    input = output;
  });
  return {filter: filters.join(';'), outputLabel: 'typographyv'};
}

function isPhotoTimelineClip(clip: TimelineClip): clip is PhotoTimelineClip {
  return clip.transform.kind === 'photo';
}

function isPhotoTimelineBRoll(overlay: TimelineBRoll): overlay is PhotoTimelineBRoll {
  return overlay.transform.kind === 'photo';
}

function brollVideoFilters(composition: CompositionSpec): {filters: string[]; outputLabel: string} {
  const filters: string[] = [];
  let baseInput = 'basev';
  composition.broll.forEach((overlay, index) => {
    const sourceLabel = `bv${index}`;
    const outputLabel = `basev_broll_${index}`;
    const inputIndex = composition.clips.length + index;
    filters.push(isPhotoTimelineBRoll(overlay)
      ? brollPhotoVideoFilter(overlay, inputIndex, sourceLabel, composition)
      : brollSourceVideoFilter(overlay, inputIndex, sourceLabel, composition));
    const start = secondsFromFrames(overlay.start_frame, composition.fps);
    const end = secondsFromFrames(overlay.start_frame + overlay.duration_frames, composition.fps);
    if (overlay.presentation === 'full_frame') {
      const overlayExpression = overlay.generated_by === 'hyperframes'
        ? `overlay=0:0:enable='between(t,${start},${end})'`
        : `overlay=0:0:enable='between(t,${start},${end})'`;
      filters.push(`[${baseInput}][${sourceLabel}]${overlayExpression}[${outputLabel}]`);
    } else {
      const layerLabel = `bo${index}`;
      filters.push(`[${sourceLabel}]${brollLayerFilter(overlay, composition)}[${layerLabel}]`);
      filters.push(`[${baseInput}][${layerLabel}]overlay=${brollPlacementExpression(overlay, composition)}:enable='between(t,${start},${end})'[${outputLabel}]`);
    }
    baseInput = outputLabel;
  });
  return {filters, outputLabel: baseInput};
}

function brollLayerFilter(overlay: TimelineBRoll, composition: CompositionSpec): string {
  const size = overlaySizeFactor(overlay.size);
  const scale = overlay.size === 'half'
    ? `scale=${Math.round(composition.resolution.width * 0.5)}:${composition.resolution.height}:force_original_aspect_ratio=decrease,pad=${Math.round(composition.resolution.width * 0.5)}:${composition.resolution.height}:(ow-iw)/2:(oh-ih)/2`
    : `scale=iw*${ffmpegNumber(size)}:ih*${ffmpegNumber(size)}`;
  const filters = [`${scale}`, 'format=rgba'];
  if (overlay.opacity < 1) {
    filters.push(`colorchannelmixer=aa=${ffmpegNumber(overlay.opacity)}`);
  }
  if (overlay.frame === 'subtle' || overlay.frame === 'bold') {
    const thickness = overlay.frame === 'bold' ? 6 : 3;
    filters.push(`drawbox=x=0:y=0:w=iw-1:h=ih-1:color=white@0.9:t=${thickness}`);
  }
  if (overlay.animation === 'subtle_scale' || overlay.animation === 'scale') {
    const progress = brollMotionProgress(overlay, composition);
    const fromScale = overlay.animation === 'scale' ? 0.82 : 0.96;
    const scale = `${ffmpegNumber(fromScale)}+(${ffmpegNumber(1 - fromScale)})*${progress}`;
    filters.push(`scale=iw*(${scale}):ih*(${scale}):eval=frame`);
  }
  if (overlay.animation === 'fade') {
    const duration = clipDurationSeconds(overlay);
    const fadeDuration = Math.min(0.35, Math.max(0.05, overlay.duration_ms / 1000 / 3));
    filters.push(`fade=t=in:st=0:d=${ffmpegNumber(fadeDuration)}:alpha=1`);
    filters.push(`fade=t=out:st=${ffmpegNumber(Math.max(0, Number(duration) - fadeDuration))}:d=${ffmpegNumber(fadeDuration)}:alpha=1`);
  }
  return filters.join(',');
}

function brollMotionProgress(overlay: TimelineBRoll, composition: CompositionSpec): string {
  const start = secondsFromFrames(overlay.start_frame, composition.fps);
  const duration = Math.min(0.45, Math.max(0.12, overlay.duration_ms / 1000 / 3));
  return `if(lt(t\\,${start}+${ffmpegNumber(duration)})\\,(t-${start})/${ffmpegNumber(duration)}\\,1)`;
}

function overlaySizeFactor(size: TimelineBRoll['size']): number {
  switch (size) {
    case 'small': return 0.28;
    case 'large': return 0.52;
    case 'half': return 0.5;
    default: return 0.38;
  }
}

function brollPlacementExpression(overlay: TimelineBRoll, composition: CompositionSpec): string {
  if (overlay.animation === 'move') {
    const progress = brollMotionProgress(overlay, composition);
    const offset = `W*0.05*(1-${progress})`;
    switch (overlay.placement) {
      case 'top_left': return `W*0.05-${offset}:H*0.05`;
      case 'top_right': return `W-w-W*0.05+${offset}:H*0.05`;
      case 'bottom_left': return `W*0.05-${offset}:H-h-H*0.08`;
      case 'center': return `(W-w)/2:(H-h)/2+${offset}`;
      case 'left': return `-${offset}:0`;
      case 'right': return `W-w+${offset}:0`;
      default: return `W-w-W*0.05+${offset}:H-h-H*0.08`;
    }
  }
  switch (overlay.placement) {
    case 'top_left': return 'W*0.05:H*0.05';
    case 'top_right': return 'W-w-W*0.05:H*0.05';
    case 'bottom_left': return 'W*0.05:H-h-H*0.08';
    case 'center': return '(W-w)/2:(H-h)/2';
    case 'left': return '0:0';
    case 'right': return 'W-w:0';
    default: return 'W-w-W*0.05:H-h-H*0.08';
  }
}

function captionVideoFilter(captions: CaptionOverlay[], requestedStyle?: CaptionStyle, inputLabel = 'basev'): string {
  if (captions.length === 0) {
    return `[${inputLabel}]format=yuv420p[outv]`;
  }
  const style = resolveCaptionStyle(requestedStyle);
  const filters: string[] = [];
  let input = inputLabel;
  captions.forEach((caption, index) => {
    const output = index === captions.length - 1 ? 'outv' : `captionv${index}`;
    filters.push(`[${input}]drawtext=${drawtextOptions(caption, style)},format=yuv420p[${output}]`);
    input = output;
  });
  return filters.join(';');
}

function drawtextOptions(caption: CaptionOverlay, style: ResolvedCaptionStyle): string {
  const options = [`textfile='${escapeFilterPath(caption.textFilePath)}'`];
  if (style.fontFamily) {
    options.push(`font='${style.fontFamily}'`);
  }
  options.push(
    `fontcolor=${captionFontColor(caption, style)}`,
    `fontsize=${captionFontSizeExpression(caption, style)}`,
    `borderw=${style.strokeWidth}`,
    `bordercolor=${style.strokeColor}@0.72`,
  );
  if (style.backgroundEnabled) {
    options.push(
      'box=1',
      `boxcolor=${style.backgroundColor}@${ffmpegNumber(style.backgroundOpacity)}`,
      `boxborderw=${Math.max(0, Math.round(style.fontSize * 0.55))}`,
    );
  } else {
    options.push('box=0');
  }
  options.push(
    'x=(w-text_w)/2',
    `y=${captionYExpression(style)}`,
    `alpha='${captionAlphaExpression(caption, style)}'`,
    `enable='between(t,${caption.startSeconds},${caption.endSeconds})'`,
  );
  return options.join(':');
}

function captionFontColor(caption: CaptionOverlay, style: ResolvedCaptionStyle): string {
  if (style.motionPreset === 'karaoke_pop') {
    return caption.motionPreset === 'karaoke_pop' ? style.highlightColor : style.textColor;
  }
  return style.textColor;
}

function captionFontSizeExpression(caption: CaptionOverlay, style: ResolvedCaptionStyle): string {
  if (style.motionPreset === 'word_pop') {
    const progress = captionMotionProgress(caption, 0.16);
    return `'${style.fontSize}*(1.18-0.18*${progress})'`;
  }
  if (style.motionPreset === 'phrase_punch') {
    const progress = captionMotionProgress(caption, 0.22);
    return `'${style.fontSize}*(1.10-0.10*${progress})'`;
  }
  if (style.motionPreset === 'karaoke_pop') {
    const progress = captionMotionProgress(caption, 0.12);
    return `'${style.fontSize}*(1.08-0.08*${progress})'`;
  }
  return String(style.fontSize);
}

function captionAlphaExpression(caption: CaptionOverlay, style: ResolvedCaptionStyle): string {
  if (style.motionPreset === 'none') {
    return '1';
  }
  const fadeInSeconds = style.motionPreset === 'phrase_punch' ? 0.12 : 0.08;
  const fadeOutSeconds = style.motionPreset === 'phrase_punch' ? 0.16 : 0.10;
  const start = Number.parseFloat(caption.startSeconds);
  const end = Number.parseFloat(caption.endSeconds);
  const duration = Math.max(0.001, end - start);
  const fadeIn = Math.min(fadeInSeconds, duration / 3);
  const fadeOut = Math.min(fadeOutSeconds, duration / 3);
  if (duration <= fadeIn + fadeOut + 0.02) {
    return '1';
  }
  const relative = `(t-${caption.startSeconds})`;
  return `if(lt(${relative}\\,${ffmpegNumber(fadeIn)})\\,${relative}/${ffmpegNumber(fadeIn)}\\,if(gt(${relative}\\,${ffmpegNumber(duration - fadeOut)})\\,(${ffmpegNumber(duration)}-${relative})/${ffmpegNumber(fadeOut)}\\,1))`;
}

function captionMotionProgress(caption: CaptionOverlay, maxRampSeconds: number): string {
  const start = Number.parseFloat(caption.startSeconds);
  const end = Number.parseFloat(caption.endSeconds);
  const duration = Math.max(0.001, end - start);
  const ramp = Math.min(maxRampSeconds, Math.max(0.04, duration / 2));
  return `min(max((t-${caption.startSeconds})/${ffmpegNumber(ramp)}\\,0)\\,1)`;
}

function typographyDrawtextOptions(overlay: TypographyOverlay): string {
  const style = resolveTypographyStyle(overlay.style);
  const fontSize = overlay.kind === 'title' ? style.titleFontSize : style.subtitleFontSize;
  const color = overlay.kind === 'title' ? style.textColor : style.subtitleColor;
  const options = [`textfile='${escapeFilterPath(overlay.textFilePath)}'`];
  options.push(
    `font='${style.fontFamily}'`,
    `fontcolor=${color}`,
    `fontsize=${fontSize}`,
    `borderw=${style.strokeWidth}`,
    `bordercolor=${style.shadowColor}@0.34`,
    `shadowcolor=${style.shadowColor}@${ffmpegNumber(style.shadowOpacity)}`,
    `shadowx=${Math.max(1, Math.round(fontSize * 0.035))}`,
    `shadowy=${Math.max(2, Math.round(fontSize * 0.045))}`,
  );
  if (style.backgroundEnabled) {
    options.push('box=1', `boxcolor=${style.backgroundColor}@${ffmpegNumber(style.backgroundOpacity)}`, `boxborderw=${Math.round(fontSize * 0.38)}`);
  } else {
    options.push('box=0');
  }
  options.push(
    `x=${typographyXExpression(overlay)}`,
    `y=${typographyYExpression(overlay, fontSize)}`,
    `alpha='${typographyAlphaExpression(overlay)}'`,
    `enable='between(t,${overlay.startSeconds},${overlay.endSeconds})'`,
  );
  return options.join(':');
}

function resolveTypographyStyle(value: string): ResolvedTypographyStyle {
  const style = value === 'cinematic' || value === 'minimal' || value === 'editorial' || value === 'memory' || value === 'location' ? value : 'cinematic';
  if (style === 'cinematic') {
    return {...DEFAULT_TYPOGRAPHY_STYLE, fontFamily: 'Liberation Serif', titleFontSize: 78, subtitleFontSize: 34, strokeWidth: 1, backgroundEnabled: false};
  }
  if (style === 'minimal') {
    return {...DEFAULT_TYPOGRAPHY_STYLE, fontFamily: 'Liberation Sans', titleFontSize: 50, subtitleFontSize: 30, shadowOpacity: 0.42, strokeWidth: 0};
  }
  if (style === 'editorial') {
    return {...DEFAULT_TYPOGRAPHY_STYLE, fontFamily: 'Liberation Serif', titleFontSize: 86, subtitleFontSize: 36, backgroundEnabled: true, backgroundOpacity: 0.18};
  }
  if (style === 'location') {
    return {...DEFAULT_TYPOGRAPHY_STYLE, fontFamily: 'Liberation Sans', titleFontSize: 34, subtitleFontSize: 26, shadowOpacity: 0.46, strokeWidth: 0};
  }
  return {...DEFAULT_TYPOGRAPHY_STYLE, fontFamily: TYPOGRAPHY_FONT_FAMILY_MAP.get(style) ?? DEFAULT_TYPOGRAPHY_STYLE.fontFamily, titleFontSize: 58, subtitleFontSize: 32, textColor: '0xFFF7EE', subtitleColor: '0xFFF7EE'};
}

function typographyXExpression(overlay: TypographyOverlay): string {
  const leftMargin = `w*${ffmpegNumber(overlay.safeArea.left)}`;
  const rightMargin = `w*${ffmpegNumber(overlay.safeArea.right)}`;
  if (overlay.placement.endsWith('_center') || overlay.placement === 'center') {
    return '(w-text_w)/2';
  }
  if (overlay.placement.endsWith('_right') || overlay.placement === 'center_right') {
    return `w-text_w-${rightMargin}`;
  }
  return leftMargin;
}

function typographyYExpression(overlay: TypographyOverlay, _fontSize: number): string {
  const base = typographyBaseYExpression(overlay);
  const offset = ffmpegNumber(overlay.offsetY);
  const entranceOffset = typographyEntranceOffsetExpression(overlay);
  return `${base}+${offset}${entranceOffset}`;
}

function typographyBaseYExpression(overlay: TypographyOverlay): string {
  const topMargin = `h*${ffmpegNumber(overlay.safeArea.top)}`;
  const bottomMargin = `h*${ffmpegNumber(overlay.safeArea.bottom)}`;
  const groupHeight = ffmpegNumber(overlay.groupHeight);
  if (overlay.placement.startsWith('top_')) {
    return topMargin;
  }
  if (overlay.placement.startsWith('center') || overlay.placement === 'center') {
    return `(h-${groupHeight})/2`;
  }
  return `h-${groupHeight}-${bottomMargin}`;
}

function typographyEntranceOffsetExpression(overlay: TypographyOverlay): string {
  if (overlay.animation === 'none' || overlay.animation === 'fade') {
    return '';
  }
  const distance = overlay.animation === 'subtle_scale' ? 8 : 18;
  const sign = overlay.animation === 'fade_down' ? '-' : '+';
  return `${sign}if(lt(t\\,${overlay.startSeconds}+0.350)\\,(${ffmpegNumber(distance)}*(1-((t-${overlay.startSeconds})/0.350)))\\,0)`;
}

function typographyAlphaExpression(overlay: TypographyOverlay): string {
  if (overlay.animation === 'none') {
    return '1';
  }
  return `if(lt(t\\,${overlay.startSeconds}+0.350)\\,(t-${overlay.startSeconds})/0.350\\,if(gt(t\\,${overlay.endSeconds}-0.250)\\,(${overlay.endSeconds}-t)/0.250\\,1))`;
}

function typographyTiming(clip: TimelineClip, typography: TextOverlay, composition: CompositionSpec): {startSeconds: string; endSeconds: string} | undefined {
  const clipStartMs = Math.round((clip.start_frame / composition.fps) * 1000);
  const startMs = clampNumber(clipStartMs + Math.round(typography.start_ms ?? 0), clipStartMs, clipStartMs + clip.duration_ms);
  const requestedDurationMs = Math.round(typography.duration_ms ?? Math.max(0, clip.duration_ms - (startMs - clipStartMs)));
  const endMs = clampNumber(startMs + requestedDurationMs, startMs, Math.min(composition.duration_ms, clipStartMs + clip.duration_ms));
  if (endMs - startMs < 250) {
    return undefined;
  }
  return {startSeconds: (startMs / 1000).toFixed(3), endSeconds: (endMs / 1000).toFixed(3)};
}

function resolveTypographySafeArea(value?: TextOverlay['safe_area']): Required<NonNullable<TextOverlay['safe_area']>> {
  return {
    top: clampNumber(value?.top ?? 0.08, 0, 0.5),
    right: clampNumber(value?.right ?? 0.08, 0, 0.5),
    bottom: clampNumber(value?.bottom ?? 0.08, 0, 0.5),
    left: clampNumber(value?.left ?? 0.08, 0, 0.5),
  };
}

function supportedTypographyPlacement(value: unknown): string {
  return typeof value === 'string' && ['top_left', 'top_center', 'top_right', 'center_left', 'center', 'center_right', 'bottom_left', 'bottom_center', 'bottom_right'].includes(value) ? value : 'bottom_left';
}

function supportedTypographyAnimation(value: unknown): string {
  return typeof value === 'string' && ['fade', 'fade_up', 'fade_down', 'subtle_scale', 'none'].includes(value) ? value : 'fade_up';
}

function normalizeTypographyText(value: string, maxLineLength: number, maxLines: number): string {
  return wrapCaptionText(value.trim().replace(/[\u0000-\u001f\u007f]/g, ' ').replace(/\s+/g, ' '), maxLineLength, maxLines);
}

function countLines(value: string): number {
  return value ? value.split('\n').length : 0;
}

function resolveCaptionStyle(style?: CaptionStyle): ResolvedCaptionStyle {
  const position = style?.position === 'top' || style?.position === 'middle' || style?.position === 'bottom' ? style.position : DEFAULT_CAPTION_STYLE.position;
  const presentation = style?.presentation === 'word_by_word' ? style.presentation : DEFAULT_CAPTION_STYLE.presentation;
  const defaultTextColor = presentation === 'word_by_word' ? '0xFFD60A' : DEFAULT_CAPTION_STYLE.textColor;
  return {
    fontFamily: fontFamily(style?.font_family),
    fontSize: wholeNumber(style?.font_size, presentation === 'word_by_word' ? 72 : DEFAULT_CAPTION_STYLE.fontSize, 12, presentation === 'word_by_word' ? 112 : 96),
    textColor: ffmpegColor(style?.text_color, defaultTextColor),
    highlightColor: ffmpegColor(style?.highlight_color, DEFAULT_CAPTION_STYLE.highlightColor),
    presentation,
    motionPreset: captionMotionPreset(style?.motion_preset),
    strokeColor: ffmpegColor(style?.stroke_color, DEFAULT_CAPTION_STYLE.strokeColor),
    strokeWidth: wholeNumber(style?.stroke_width, DEFAULT_CAPTION_STYLE.strokeWidth, 0, 12),
    backgroundEnabled: style?.background_enabled ?? DEFAULT_CAPTION_STYLE.backgroundEnabled,
    backgroundColor: ffmpegColor(style?.background_color, DEFAULT_CAPTION_STYLE.backgroundColor),
    backgroundOpacity: clampNumber(style?.background_opacity ?? DEFAULT_CAPTION_STYLE.backgroundOpacity, 0, 1),
    position,
    bottomOffset: wholeNumber(style?.bottom_offset, DEFAULT_CAPTION_STYLE.bottomOffset, 0, 480),
    maxLines: wholeNumber(style?.max_lines, DEFAULT_CAPTION_STYLE.maxLines, 1, 4),
    maxCharactersPerLine: wholeNumber(style?.max_characters_per_line, DEFAULT_CAPTION_STYLE.maxCharactersPerLine, 16, 48),
  };
}

function captionMotionPreset(value: unknown): ResolvedCaptionStyle['motionPreset'] {
  if (typeof value !== 'string') {
    return DEFAULT_CAPTION_STYLE.motionPreset;
  }
  const normalized = value.trim().toLowerCase().replace(/[-\s]+/g, '_');
  if (normalized === 'word_pop' || normalized === 'karaoke_pop' || normalized === 'phrase_punch') {
    return normalized;
  }
  return DEFAULT_CAPTION_STYLE.motionPreset;
}

function fontFamily(value: unknown): string | undefined {
  if (typeof value !== 'string') {
    return DEFAULT_CAPTION_STYLE.fontFamily;
  }
  return FONT_FAMILY_MAP.get(value.trim().toLowerCase()) ?? DEFAULT_CAPTION_STYLE.fontFamily;
}

function ffmpegColor(value: unknown, fallback: string): string {
  if (typeof value !== 'string') {
    return fallback;
  }
  const trimmed = value.trim();
  return /^#[0-9a-fA-F]{6}$/.test(trimmed) ? `0x${trimmed.slice(1).toUpperCase()}` : fallback;
}

function wholeNumber(value: unknown, fallback: number, min: number, max: number): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return fallback;
  }
  return Math.round(clampNumber(value, min, max));
}

function captionYExpression(style: ResolvedCaptionStyle): string {
  if (style.position === 'top') {
    return String(style.bottomOffset);
  }
  if (style.position === 'middle') {
    return '(h-text_h)/2';
  }
  return `h-text_h-${style.bottomOffset}`;
}

function escapeFilterPath(filePath: string): string {
  return filePath.replace(/\\/g, '\\\\').replace(/'/g, `\\'`).replace(/:/g, '\\:');
}

function sourceVideoFilter(clip: TimelineClip, index: number, composition: CompositionSpec, effects: ClipTransitionEffects): string {
  const scaleAndCrop = clip.transform.kind === 'video' && clip.transform.crop === 'fit'
    ? `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=decrease,pad=${composition.resolution.width}:${composition.resolution.height}:(ow-iw)/2:(oh-ih)/2`
    : `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=increase,crop=${composition.resolution.width}:${composition.resolution.height}`;
  return `[${index}:v]${scaleAndCrop},setsar=1,fps=${composition.fps},trim=duration=${clipDurationSeconds(clip)},setpts=PTS-STARTPTS${videoFadeFilters(clip, effects)}[v${index}]`;
}

function sourceAudioFilter(clip: TimelineClip, index: number, composition: CompositionSpec, effects: ClipTransitionEffects): string {
  const volume = ffmpegNumber(clip.volume * composition.audio.original_volume);
  return `[${index}:a]atrim=duration=${clipDurationSeconds(clip)},asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=${volume}${audioFadeFilters(clip, effects)}[a${index}]`;
}

function silentAudioFilter(clip: TimelineClip, index: number, effects: ClipTransitionEffects): string {
  return `anullsrc=channel_layout=stereo:sample_rate=48000,atrim=duration=${clipDurationSeconds(clip)},asetpts=PTS-STARTPTS,aformat=sample_fmts=fltp:channel_layouts=stereo${audioFadeFilters(clip, effects)}[a${index}]`;
}

function photoVideoFilter(clip: PhotoTimelineClip, index: number, composition: CompositionSpec, effects: ClipTransitionEffects): string {
  const transform = clip.transform;
  const denominator = Math.max(1, clip.duration_frames - 1);
  const progress = `on/${denominator}`;
  const fromScale = ffmpegNumber(transform.from_scale);
  const toScale = ffmpegNumber(transform.to_scale);
  const fromX = ffmpegNumber(transform.from_x);
  const toX = ffmpegNumber(transform.to_x);
  const fromY = ffmpegNumber(transform.from_y);
  const toY = ffmpegNumber(transform.to_y);
  const zoom = `${fromScale}+(${toScale}-${fromScale})*${progress}`;
  const x = `iw/2-(iw/zoom/2)+(${fromX}+(${toX}-${fromX})*${progress})*iw`;
  const y = `ih/2-(ih/zoom/2)+(${fromY}+(${toY}-${fromY})*${progress})*ih`;
  return `[${index}:v]scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=increase,crop=${composition.resolution.width}:${composition.resolution.height},setsar=1,fps=${composition.fps},zoompan=z='${zoom}':x='${x}':y='${y}':d=1:s=${composition.resolution.width}x${composition.resolution.height}:fps=${composition.fps},trim=duration=${clipDurationSeconds(clip)},setpts=PTS-STARTPTS${videoFadeFilters(clip, effects)}[v${index}]`;
}

function brollSourceVideoFilter(overlay: TimelineBRoll, inputIndex: number, outputLabel: string, composition: CompositionSpec): string {
  if (overlay.generated_by === 'hyperframes') {
    return `[${inputIndex}:v]scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=decrease,pad=${composition.resolution.width}:${composition.resolution.height}:(ow-iw)/2:(oh-ih)/2,format=rgba,setsar=1,fps=${composition.fps},trim=duration=${clipDurationSeconds(overlay)},setpts=PTS-STARTPTS+${secondsFromFrames(overlay.start_frame, composition.fps)}/TB[${outputLabel}]`;
  }
  const scaleAndCrop = overlay.transform.kind === 'video' && overlay.transform.crop === 'fit'
    ? `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=decrease,pad=${composition.resolution.width}:${composition.resolution.height}:(ow-iw)/2:(oh-ih)/2`
    : `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=increase,crop=${composition.resolution.width}:${composition.resolution.height}`;
  return `[${inputIndex}:v]${scaleAndCrop},setsar=1,fps=${composition.fps},trim=duration=${clipDurationSeconds(overlay)},setpts=PTS-STARTPTS+${secondsFromFrames(overlay.start_frame, composition.fps)}/TB[${outputLabel}]`;
}

function brollPhotoVideoFilter(overlay: PhotoTimelineBRoll, inputIndex: number, outputLabel: string, composition: CompositionSpec): string {
  const transform = overlay.transform;
  const denominator = Math.max(1, overlay.duration_frames - 1);
  const progress = `on/${denominator}`;
  const fromScale = ffmpegNumber(transform.from_scale);
  const toScale = ffmpegNumber(transform.to_scale);
  const fromX = ffmpegNumber(transform.from_x);
  const toX = ffmpegNumber(transform.to_x);
  const fromY = ffmpegNumber(transform.from_y);
  const toY = ffmpegNumber(transform.to_y);
  const zoom = `${fromScale}+(${toScale}-${fromScale})*${progress}`;
  const x = `iw/2-(iw/zoom/2)+(${fromX}+(${toX}-${fromX})*${progress})*iw`;
  const y = `ih/2-(ih/zoom/2)+(${fromY}+(${toY}-${fromY})*${progress})*ih`;
  return `[${inputIndex}:v]scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=increase,crop=${composition.resolution.width}:${composition.resolution.height},setsar=1,fps=${composition.fps},zoompan=z='${zoom}':x='${x}':y='${y}':d=1:s=${composition.resolution.width}x${composition.resolution.height}:fps=${composition.fps},trim=duration=${clipDurationSeconds(overlay)},setpts=PTS-STARTPTS+${secondsFromFrames(overlay.start_frame, composition.fps)}/TB[${outputLabel}]`;
}

function transitionEffects(clips: TimelineClip[], index: number): ClipTransitionEffects {
  const clip = clips[index];
  return {
    fadeInSeconds: index > 0 && isFadeTransition(clip.transition) ? fadeDurationSeconds(clip) : 0,
    fadeOutSeconds: index < clips.length - 1 && isFadeTransition(clips[index + 1].transition) ? fadeDurationSeconds(clip) : 0,
  };
}

function isFadeTransition(transition: string): boolean {
  return transition === 'fade' || transition === 'dissolve' || transition === 'crossfade';
}

function fadeDurationSeconds(clip: {duration_ms: number}): number {
  return Math.max(0.001, Math.min(SCENE_FADE_SECONDS, Math.max(0.001, clip.duration_ms / 3000)));
}

function videoFadeFilters(clip: {duration_ms: number}, effects: ClipTransitionEffects): string {
  const filters: string[] = [];
  if (effects.fadeInSeconds > 0) {
    filters.push(`fade=t=in:st=0:d=${ffmpegNumber(effects.fadeInSeconds)}`);
  }
  if (effects.fadeOutSeconds > 0) {
    filters.push(`fade=t=out:st=${fadeOutStartSeconds(clip, effects.fadeOutSeconds)}:d=${ffmpegNumber(effects.fadeOutSeconds)}`);
  }
  return filters.length > 0 ? `,${filters.join(',')}` : '';
}

function audioFadeFilters(clip: {duration_ms: number}, effects: ClipTransitionEffects): string {
  const filters: string[] = [];
  if (effects.fadeInSeconds > 0) {
    filters.push(`afade=t=in:st=0:d=${ffmpegNumber(effects.fadeInSeconds)}`);
  }
  if (effects.fadeOutSeconds > 0) {
    filters.push(`afade=t=out:st=${fadeOutStartSeconds(clip, effects.fadeOutSeconds)}:d=${ffmpegNumber(effects.fadeOutSeconds)}`);
  }
  return filters.length > 0 ? `,${filters.join(',')}` : '';
}

function fadeOutStartSeconds(clip: {duration_ms: number}, fadeSeconds: number): string {
  return ffmpegNumber(Math.max(0, (clip.duration_ms / 1000) - fadeSeconds));
}

function clipDurationSeconds(clip: {duration_ms: number}): string {
  return Math.max(0.001, clip.duration_ms / 1000).toFixed(3);
}

function brollDurationSeconds(overlay: TimelineBRoll): string {
  return clipDurationSeconds(overlay);
}

function secondsFromFrames(frames: number, fps: number): string {
  return (frames / fps).toFixed(3);
}

function ffmpegNumber(value: number): string {
  return Number.isFinite(value) ? value.toFixed(4) : '0.0000';
}

function normalizeCaptionText(value: string, style: ResolvedCaptionStyle): string {
  return wrapCaptionText(value.trim().replace(/\s+/g, ' ').slice(0, 120), style.maxCharactersPerLine, style.maxLines);
}

function captionPages(value: string, style: ResolvedCaptionStyle): string[] {
  const normalized = value.trim().replace(/\s+/g, ' ').slice(0, 120);
  if (!normalized) {
    return [];
  }
  const words = normalized.split(' ');
  const pages: string[] = [];
  let lines: string[] = [];
  let current = '';
  const flushPage = () => {
    if (current) {
      lines.push(current);
      current = '';
    }
    if (lines.length > 0) {
      pages.push(lines.join('\n'));
      lines = [];
    }
  };
  for (const word of words) {
    const next = current ? `${current} ${word}` : word;
    if (next.length <= style.maxCharactersPerLine || !current) {
      current = next;
      continue;
    }
    lines.push(current);
    current = word;
    if (lines.length === style.maxLines) {
      pages.push(lines.join('\n'));
      lines = [];
    }
  }
  flushPage();
  return pages;
}

function wrapCaptionText(value: string, maxLineLength: number, maxLines: number): string {
  if (!value) {
    return '';
  }
  const words = value.split(' ');
  const lines: string[] = [];
  let current = '';
  for (const word of words) {
    const next = current ? `${current} ${word}` : word;
    if (next.length <= maxLineLength || !current) {
      current = next;
      continue;
    }
    lines.push(current);
    current = word;
    if (lines.length === maxLines) {
      break;
    }
  }
  if (current && lines.length < maxLines) {
    lines.push(current);
  }
  const wrapped = lines.slice(0, maxLines).join('\n');
  return wrapped.length < value.length ? `${wrapped.trimEnd()}…` : wrapped;
}

function captionTiming(caption: StoryCaption, composition: CompositionSpec): {startSeconds: string; endSeconds: string} | undefined {
  const fallbackClip = composition.clips.find((clip) => clip.clip_id === caption.moment_id || clip.asset_id === caption.moment_id);
  const fallbackStartMs = fallbackClip ? Math.round((fallbackClip.start_frame / composition.fps) * 1000) : 0;
  const fallbackEndMs = fallbackClip ? fallbackStartMs + fallbackClip.duration_ms : composition.duration_ms;
  const startMs = clampNumber(caption.start_ms ?? fallbackStartMs, 0, composition.duration_ms);
  const endMs = clampNumber(caption.end_ms ?? fallbackEndMs, startMs, composition.duration_ms);
  if (endMs - startMs < 250) {
    return undefined;
  }
  return {startSeconds: (startMs / 1000).toFixed(3), endSeconds: (endMs / 1000).toFixed(3)};
}

function expandCaptionTextAndTiming(text: string, timing: {startSeconds: string; endSeconds: string}, style: ResolvedCaptionStyle): Array<Omit<CaptionOverlay, 'textFilePath'>> {
  if (style.presentation !== 'word_by_word') {
    const pages = captionPages(text, style);
    if (pages.length <= 1) {
      return [{text: normalizeCaptionText(text, style), startSeconds: timing.startSeconds, endSeconds: timing.endSeconds, sequenceStartSeconds: timing.startSeconds, sequenceEndSeconds: timing.endSeconds, motionPreset: style.motionPreset}];
    }
    const start = Number.parseFloat(timing.startSeconds);
    const end = Number.parseFloat(timing.endSeconds);
    const duration = Math.max(0.25, end - start);
    const step = duration / pages.length;
    return pages.map((page, index) => ({
      text: page,
      startSeconds: (start + (step * index)).toFixed(3),
      endSeconds: (index === pages.length - 1 ? end : start + (step * (index + 1))).toFixed(3),
      sequenceStartSeconds: (start + (step * index)).toFixed(3),
      sequenceEndSeconds: (index === pages.length - 1 ? end : start + (step * (index + 1))).toFixed(3),
      motionPreset: style.motionPreset,
    }));
  }
  const words = text.trim().replace(/\s+/g, ' ').split(' ').map((word) => word.trim()).filter(Boolean);
  if (words.length <= 1) {
    return [{text: words[0] ?? '', startSeconds: timing.startSeconds, endSeconds: timing.endSeconds, sequenceStartSeconds: timing.startSeconds, sequenceEndSeconds: timing.endSeconds, motionPreset: style.motionPreset}];
  }
  const start = Number.parseFloat(timing.startSeconds);
  const end = Number.parseFloat(timing.endSeconds);
  const duration = Math.max(0.25, end - start);
  const chunks = captionWordChunks(words, style);
  const step = duration / chunks.length;
  const normalizedText = normalizeCaptionText(text, style);
  return chunks.map((chunk, index) => ({
    text: style.motionPreset === 'karaoke_pop' ? normalizedText : chunk.join(' '),
    startSeconds: (start + (step * index)).toFixed(3),
    endSeconds: (index === chunks.length - 1 ? end : start + (step * (index + 1))).toFixed(3),
    sequenceStartSeconds: timing.startSeconds,
    sequenceEndSeconds: timing.endSeconds,
    motionPreset: style.motionPreset,
  }));
}

function captionWordChunks(words: string[], style: ResolvedCaptionStyle): string[][] {
  if (style.maxLines <= 1 && style.maxCharactersPerLine <= 18) {
    return chunkWords(words, 2, style.maxCharactersPerLine);
  }
  return chunkWords(words, 3, Math.max(18, style.maxCharactersPerLine));
}

function chunkWords(words: string[], maxWords: number, maxCharacters: number): string[][] {
  const chunks: string[][] = [];
  let current: string[] = [];
  for (const word of words) {
    const next = [...current, word];
    const nextText = next.join(' ');
    if (current.length > 0 && (next.length > maxWords || nextText.length > maxCharacters)) {
      chunks.push(current);
      current = [word];
      continue;
    }
    current = next;
  }
  if (current.length > 0) {
    chunks.push(current);
  }
  return chunks.length > 0 ? chunks : [words];
}

function clampNumber(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) {
    return min;
  }
  return Math.max(min, Math.min(max, value));
}

function isHeifImagePath(filePath: string): boolean {
  const extension = path.extname(filePath).toLowerCase();
  return extension === '.heic' || extension === '.heif';
}

async function isJpegImage(filePath: string): Promise<boolean> {
  let file: Awaited<ReturnType<typeof open>> | undefined;
  try {
    file = await open(filePath, 'r');
    const header = Buffer.alloc(3);
    const result = await file.read(header, 0, header.length, 0);
    return result.bytesRead >= 3 && header[0] === 0xff && header[1] === 0xd8 && header[2] === 0xff;
  } catch {
    return false;
  } finally {
    await file?.close();
  }
}

async function run(command: string, args: string[], timeoutMs: number): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, args, {stdio: ['ignore', 'pipe', 'pipe']});
    const timeout = setTimeout(() => {
      child.kill('SIGTERM');
      reject(new Error(`${command} timed out after ${timeoutMs}ms`));
    }, timeoutMs);
    let stderr = '';
    child.stderr.on('data', (chunk) => {
      stderr += String(chunk);
    });
    child.on('error', (error) => {
      clearTimeout(timeout);
      reject(error);
    });
    child.on('close', (code) => {
      clearTimeout(timeout);
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} exited with ${code}: ${stderr}`));
    });
  });
}


export interface FfmpegCompositionRendererOptions {
  ffmpegBinary?: string;
  heifConvertBinary?: string;
}

export class FfmpegCompositionRenderer implements RemotionRenderer {
  private readonly ffmpegBinary: string;
  private readonly heifConvertBinary: string;

  constructor(options: FfmpegCompositionRendererOptions = {}) {
    this.ffmpegBinary = options.ffmpegBinary ?? 'ffmpeg';
    this.heifConvertBinary = options.heifConvertBinary ?? 'heif-convert';
  }

  async renderComposition(composition: CompositionSpec, outputPath: string): Promise<void> {
    await mkdir(path.dirname(outputPath), {recursive: true});
    const clip = composition.clips[0];
    const durationSeconds = Math.max(0.1, composition.duration_ms / 1000);
    if (!clip) {
      await run(this.ffmpegBinary, [
        '-hide_banner', '-loglevel', 'error', '-y',
        '-f', 'lavfi', '-i', `color=c=0x111111:s=${composition.resolution.width}x${composition.resolution.height}:r=${composition.fps}:d=${durationSeconds.toFixed(3)}`,
        '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', outputPath,
      ], 15 * 60 * 1000);
      return;
    }
    const args = ['-hide_banner', '-loglevel', 'error', '-y'];
    for (const timelineClip of composition.clips) {
      const inputPath = timelineClip.media_type === 'video'
        ? timelineClip.prepared_path
        : await this.prepareStillImageInput(timelineClip, outputPath);
      if (timelineClip.media_type !== 'video') {
        args.push('-loop', '1', '-t', clipDurationSeconds(timelineClip));
      }
      args.push('-i', inputPath);
    }
    for (const overlay of composition.broll) {
      const inputPath = overlay.media_type === 'video'
        ? overlay.prepared_path
        : await this.prepareStillImageInput(overlay, outputPath);
      if (overlay.media_type !== 'video') {
        args.push('-loop', '1', '-t', brollDurationSeconds(overlay));
      }
      args.push('-i', inputPath);
    }
    const captions = await this.prepareCaptionOverlays(composition, outputPath);
    const typography = await this.prepareTypographyOverlays(composition, outputPath);
    const captionStyle = resolveCaptionStyle(composition.caption_style);
    console.info(JSON.stringify({
      level: 'info',
      msg: 'ffmpeg overlays prepared',
      project_id: composition.project_id,
      story_plan_id: composition.story_plan_id,
      input_caption_count: composition.captions.length,
      caption_overlay_count: captions.length,
      typography_overlay_count: typography.length,
      caption_style_preset: composition.caption_style?.preset ?? 'unset',
      caption_style_presentation_requested: composition.caption_style?.presentation ?? 'unset',
      caption_style_presentation_resolved: captionStyle.presentation,
      caption_style_motion_requested: composition.caption_style?.motion_preset ?? 'unset',
      caption_style_motion_resolved: captionStyle.motionPreset,
    }));
    const filter = concatFilter(composition, captions, typography);
    console.info(JSON.stringify({
      level: 'info',
      msg: 'ffmpeg audio mapping resolved',
      project_id: composition.project_id,
      story_plan_id: composition.story_plan_id,
      audio_source: composition.audio.source,
      original_audio: composition.audio.original_audio,
      original_volume: composition.audio.original_volume,
      video_clip_count: composition.clips.filter((clip) => clip.media_type === 'video').length,
      audio_eligible_video_clip_count: composition.clips.filter((clip) => clip.media_type === 'video' && clip.volume > 0).length,
      has_audio_output: filter.hasAudio,
      output_mapping: filter.hasAudio ? '[outa]' : 'none',
    }));
    args.push(
      '-filter_complex',
      filter.filter,
      '-map',
      '[outv]',
    );
    if (filter.hasAudio) {
      args.push('-map', '[outa]', '-c:a', 'aac', '-b:a', '192k');
    } else {
      args.push('-an');
    }
    args.push(
      '-t',
      durationSeconds.toFixed(3),
      '-r',
      String(composition.fps),
      '-c:v',
      'libx264',
      '-pix_fmt',
      'yuv420p',
      '-movflags',
      '+faststart',
      outputPath,
    );
    await run(this.ffmpegBinary, args, 15 * 60 * 1000);
  }

  private async prepareCaptionOverlays(composition: CompositionSpec, outputPath: string): Promise<CaptionOverlay[]> {
    const overlays: CaptionOverlay[] = [];
    const captionsDir = path.dirname(outputPath);
    const captionStyle = resolveCaptionStyle(composition.caption_style);
    for (const [index, caption] of composition.captions.entries()) {
      const sourceText = caption.text.trim().replace(/\s+/g, ' ');
      if (!sourceText) {
        console.info(JSON.stringify({level: 'info', msg: 'skipping empty caption overlay', project_id: composition.project_id, story_plan_id: composition.story_plan_id, caption_index: index, moment_id: caption.moment_id}));
        continue;
      }
      const timing = captionTiming(caption, composition);
      if (!timing) {
        console.info(JSON.stringify({level: 'info', msg: 'skipping caption overlay with invalid timing', project_id: composition.project_id, story_plan_id: composition.story_plan_id, caption_index: index, moment_id: caption.moment_id, start_ms: caption.start_ms ?? null, end_ms: caption.end_ms ?? null, duration_ms: composition.duration_ms}));
        continue;
      }
      for (const [partIndex, part] of expandCaptionTextAndTiming(sourceText, timing, captionStyle).entries()) {
        if (!part.text) {
          continue;
        }
        const textFilePath = path.join(captionsDir, `caption-${index}-${partIndex}-${stableHash(part).slice(0, 12)}.txt`);
        await writeFile(textFilePath, part.text, 'utf8');
        overlays.push({...part, textFilePath});
      }
    }
    return overlays;
  }

  private async prepareTypographyOverlays(composition: CompositionSpec, outputPath: string): Promise<TypographyOverlay[]> {
    const overlays: TypographyOverlay[] = [];
    const outputDir = path.dirname(outputPath);
    for (const [clipIndex, clip] of (composition.scenes ?? composition.clips).entries()) {
      const textOverlays = clip.overlays?.length ? clip.overlays : clip.typography ? [clip.typography] : [];
      for (const [overlayIndex, typography] of textOverlays.entries()) {
        if (!typography?.enabled) {
          continue;
        }
      const timing = typographyTiming(clip, typography, composition);
      if (!timing) {
        console.info(JSON.stringify({level: 'info', msg: 'skipping typography overlay with invalid timing', project_id: composition.project_id, story_plan_id: composition.story_plan_id, clip_id: clip.clip_id}));
        continue;
      }
      const safeArea = resolveTypographySafeArea(typography.safe_area);
      const style = resolveTypographyStyle(String(typography.style ?? 'cinematic'));
      const maxWidth = clampNumber(typography.max_width ?? 0.72, 0.2, 0.95);
      const placement = supportedTypographyPlacement(typography.placement);
      const animation = supportedTypographyAnimation(typography.animation);
      const title = normalizeTypographyText(typography.title ?? '', Math.max(12, Math.floor((composition.resolution.width * maxWidth) / (style.titleFontSize * 0.52))), 2);
      const subtitle = normalizeTypographyText(typography.subtitle ?? '', Math.max(18, Math.floor((composition.resolution.width * maxWidth) / (style.subtitleFontSize * 0.54))), 2);
      const titleLines = countLines(title);
      const subtitleLines = countLines(subtitle);
      const titleHeight = titleLines * style.titleFontSize * 1.08;
      const subtitleHeight = subtitleLines * style.subtitleFontSize * 1.18;
      const gap = titleLines > 0 && subtitleLines > 0 ? Math.round(style.subtitleFontSize * 0.42) : 0;
      const groupHeight = titleHeight + gap + subtitleHeight;
      if (title) {
        const textFilePath = path.join(outputDir, `typography-${clipIndex}-${overlayIndex}-title-${stableHash({title, timing}).slice(0, 12)}.txt`);
        await writeFile(textFilePath, title, 'utf8');
        overlays.push({textFilePath, startSeconds: timing.startSeconds, endSeconds: timing.endSeconds, placement, style: String(typography.style ?? 'cinematic'), kind: 'title', safeArea, maxWidth, lineCount: titleLines, groupHeight, offsetY: 0, animation});
      }
      if (subtitle) {
        const textFilePath = path.join(outputDir, `typography-${clipIndex}-${overlayIndex}-subtitle-${stableHash({subtitle, timing}).slice(0, 12)}.txt`);
        await writeFile(textFilePath, subtitle, 'utf8');
        overlays.push({textFilePath, startSeconds: timing.startSeconds, endSeconds: timing.endSeconds, placement, style: String(typography.style ?? 'cinematic'), kind: 'subtitle', safeArea, maxWidth, lineCount: subtitleLines, groupHeight, offsetY: titleHeight + gap, animation});
      }
      }
    }
    return overlays;
  }

  private async prepareStillImageInput(clip: {source_path: string; clip_id?: string; broll_id?: string}, outputPath: string): Promise<string> {
    if (!isHeifImagePath(clip.source_path)) {
      return clip.source_path;
    }
    const clipID = clip.clip_id ?? clip.broll_id ?? stableHash(clip.source_path).slice(0, 20);
    if (await isJpegImage(clip.source_path)) {
      console.warn(JSON.stringify({level: 'warn', msg: 'HEIC extension contains JPEG bytes; using source image directly', clip_id: clipID, source_path: clip.source_path}));
      return clip.source_path;
    }
    const convertedPath = path.join(path.dirname(outputPath), `${clipID}.jpg`);
    try {
      await run(this.heifConvertBinary, [clip.source_path, convertedPath], 60 * 1000);
    } catch (error) {
      if (error instanceof Error && error.message.includes('is a JPEG image')) {
        console.warn(JSON.stringify({level: 'warn', msg: 'HEIC converter reported JPEG bytes; using source image directly', clip_id: clipID, source_path: clip.source_path}));
        return clip.source_path;
      }
      throw error;
    }
    return convertedPath;
  }
}
