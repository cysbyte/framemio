import {spawn} from 'node:child_process';
import {mkdir, open, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import type {CaptionStyle, CompositionSpec, KenBurnsTransform, StoryCaption, TimelineClip} from '../types.js';
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
    const propsPath = path.join(tmpdir(), `framemio-remotion-${stableHash(composition).slice(0, 20)}.json`);
    await writeFile(propsPath, canonicalJson({composition}), 'utf8');
    const args = this.remotionBinary === 'npx'
      ? ['remotion', 'render', this.options.entryPoint, this.compositionID, outputPath]
      : ['render', this.options.entryPoint, this.compositionID, outputPath];
    args.push('--props', propsPath, '--codec', 'h264', '--overwrite', '--concurrency', '1');
    await run(this.remotionBinary, args, this.timeoutMs);
  }
}

type PhotoTimelineClip = TimelineClip & {transform: KenBurnsTransform};

interface ConcatFilter {
  filter: string;
  hasAudio: boolean;
}

interface CaptionOverlay {
  textFilePath: string;
  startSeconds: string;
  endSeconds: string;
}

interface ResolvedCaptionStyle {
  fontFamily?: string;
  fontSize: number;
  textColor: string;
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

const DEFAULT_CAPTION_STYLE: ResolvedCaptionStyle = {
  fontSize: 44,
  textColor: 'white',
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
  ['inter', 'DejaVu Sans'],
  ['inter-bold', 'DejaVu Sans Bold'],
  ['system', 'DejaVu Sans'],
  ['system-bold', 'DejaVu Sans Bold'],
  ['noto-sans', 'DejaVu Sans'],
  ['noto-sans-bold', 'DejaVu Sans Bold'],
]);

const SCENE_FADE_SECONDS = 0.45;

interface ClipTransitionEffects {
  fadeInSeconds: number;
  fadeOutSeconds: number;
}

function concatFilter(composition: CompositionSpec, captions: CaptionOverlay[] = []): ConcatFilter {
  const videoFilters = composition.clips.map((clip, index) => isPhotoTimelineClip(clip)
    ? photoVideoFilter(clip, index, composition, transitionEffects(composition.clips, index))
    : sourceVideoFilter(clip, index, composition, transitionEffects(composition.clips, index)));
  const videoInputs = composition.clips.map((_, index) => `[v${index}]`).join('');
  const filters = [...videoFilters, `${videoInputs}concat=n=${composition.clips.length}:v=1:a=0,format=yuv420p[basev]`];
  filters.push(captionVideoFilter(captions, composition.caption_style));
  if (!composition.audio.original_audio) {
    return {filter: filters.join(';'), hasAudio: false};
  }

  const audioFilters = composition.clips.map((clip, index) => isPhotoTimelineClip(clip)
    ? silentAudioFilter(clip, index, transitionEffects(composition.clips, index))
    : sourceAudioFilter(clip, index, composition, transitionEffects(composition.clips, index)));
  const audioInputs = composition.clips.map((_, index) => `[a${index}]`).join('');
  filters.push(...audioFilters, `${audioInputs}concat=n=${composition.clips.length}:v=0:a=1[outa]`);
  return {filter: filters.join(';'), hasAudio: true};
}

function isPhotoTimelineClip(clip: TimelineClip): clip is PhotoTimelineClip {
  return clip.transform.kind === 'photo';
}

function captionVideoFilter(captions: CaptionOverlay[], requestedStyle?: CaptionStyle): string {
  if (captions.length === 0) {
    return '[basev]format=yuv420p[outv]';
  }
  const style = resolveCaptionStyle(requestedStyle);
  const filters: string[] = [];
  let input = 'basev';
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
    `fontcolor=${style.textColor}`,
    `fontsize=${style.fontSize}`,
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
    `enable='between(t,${caption.startSeconds},${caption.endSeconds})'`,
  );
  return options.join(':');
}

function resolveCaptionStyle(style?: CaptionStyle): ResolvedCaptionStyle {
  const position = style?.position === 'top' || style?.position === 'middle' || style?.position === 'bottom' ? style.position : DEFAULT_CAPTION_STYLE.position;
  return {
    fontFamily: fontFamily(style?.font_family),
    fontSize: wholeNumber(style?.font_size, DEFAULT_CAPTION_STYLE.fontSize, 12, 96),
    textColor: ffmpegColor(style?.text_color, DEFAULT_CAPTION_STYLE.textColor),
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

function ffmpegNumber(value: number): string {
  return Number.isFinite(value) ? value.toFixed(4) : '0.0000';
}

function normalizeCaptionText(value: string, style: ResolvedCaptionStyle): string {
  return wrapCaptionText(value.trim().replace(/\s+/g, ' ').slice(0, 120), style.maxCharactersPerLine, style.maxLines);
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
    const captions = await this.prepareCaptionOverlays(composition, outputPath);
    console.info(JSON.stringify({level: 'info', msg: 'ffmpeg caption overlays prepared', project_id: composition.project_id, story_plan_id: composition.story_plan_id, input_caption_count: composition.captions.length, overlay_count: captions.length}));
    const filter = concatFilter(composition, captions);
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
      const text = normalizeCaptionText(caption.text, captionStyle);
      if (!text) {
        console.info(JSON.stringify({level: 'info', msg: 'skipping empty caption overlay', project_id: composition.project_id, story_plan_id: composition.story_plan_id, caption_index: index, moment_id: caption.moment_id}));
        continue;
      }
      const timing = captionTiming(caption, composition);
      if (!timing) {
        console.info(JSON.stringify({level: 'info', msg: 'skipping caption overlay with invalid timing', project_id: composition.project_id, story_plan_id: composition.story_plan_id, caption_index: index, moment_id: caption.moment_id, start_ms: caption.start_ms ?? null, end_ms: caption.end_ms ?? null, duration_ms: composition.duration_ms}));
        continue;
      }
      const textFilePath = path.join(captionsDir, `caption-${index}-${stableHash({text, timing}).slice(0, 12)}.txt`);
      await writeFile(textFilePath, text, 'utf8');
      overlays.push({textFilePath, startSeconds: timing.startSeconds, endSeconds: timing.endSeconds});
    }
    return overlays;
  }

  private async prepareStillImageInput(clip: TimelineClip, outputPath: string): Promise<string> {
    if (!isHeifImagePath(clip.source_path)) {
      return clip.source_path;
    }
    if (await isJpegImage(clip.source_path)) {
      console.warn(JSON.stringify({level: 'warn', msg: 'HEIC extension contains JPEG bytes; using source image directly', clip_id: clip.clip_id, source_path: clip.source_path}));
      return clip.source_path;
    }
    const convertedPath = path.join(path.dirname(outputPath), `${clip.clip_id}.jpg`);
    try {
      await run(this.heifConvertBinary, [clip.source_path, convertedPath], 60 * 1000);
    } catch (error) {
      if (error instanceof Error && error.message.includes('is a JPEG image')) {
        console.warn(JSON.stringify({level: 'warn', msg: 'HEIC converter reported JPEG bytes; using source image directly', clip_id: clip.clip_id, source_path: clip.source_path}));
        return clip.source_path;
      }
      throw error;
    }
    return convertedPath;
  }
}
