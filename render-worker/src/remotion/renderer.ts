import {spawn} from 'node:child_process';
import {mkdir, open, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import type {CompositionSpec, KenBurnsTransform, StoryCaption, TimelineClip} from '../types.js';
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

function concatFilter(composition: CompositionSpec, captions: CaptionOverlay[] = []): ConcatFilter {
  const videoFilters = composition.clips.map((clip, index) => isPhotoTimelineClip(clip)
    ? photoVideoFilter(clip, index, composition)
    : sourceVideoFilter(clip, index, composition));
  const videoInputs = composition.clips.map((_, index) => `[v${index}]`).join('');
  const filters = [...videoFilters, `${videoInputs}concat=n=${composition.clips.length}:v=1:a=0,format=yuv420p[basev]`];
  filters.push(captionVideoFilter(captions));
  if (!composition.audio.original_audio) {
    return {filter: filters.join(';'), hasAudio: false};
  }

  const audioFilters = composition.clips.map((clip, index) => isPhotoTimelineClip(clip)
    ? silentAudioFilter(clip, index)
    : sourceAudioFilter(clip, index, composition));
  const audioInputs = composition.clips.map((_, index) => `[a${index}]`).join('');
  filters.push(...audioFilters, `${audioInputs}concat=n=${composition.clips.length}:v=0:a=1[outa]`);
  return {filter: filters.join(';'), hasAudio: true};
}

function isPhotoTimelineClip(clip: TimelineClip): clip is PhotoTimelineClip {
  return clip.transform.kind === 'photo';
}

function captionVideoFilter(captions: CaptionOverlay[]): string {
  if (captions.length === 0) {
    return '[basev]format=yuv420p[outv]';
  }
  const filters: string[] = [];
  let input = 'basev';
  captions.forEach((caption, index) => {
    const output = index === captions.length - 1 ? 'outv' : `captionv${index}`;
    filters.push(`[${input}]drawtext=textfile='${escapeFilterPath(caption.textFilePath)}':fontcolor=white:fontsize=44:borderw=3:bordercolor=black@0.72:box=1:boxcolor=black@0.32:boxborderw=24:x=(w-text_w)/2:y=h-text_h-150:enable='between(t,${caption.startSeconds},${caption.endSeconds})',format=yuv420p[${output}]`);
    input = output;
  });
  return filters.join(';');
}

function escapeFilterPath(filePath: string): string {
  return filePath.replace(/\\/g, '\\\\').replace(/'/g, `\\'`).replace(/:/g, '\\:');
}

function sourceVideoFilter(clip: TimelineClip, index: number, composition: CompositionSpec): string {
  const scaleAndCrop = clip.transform.kind === 'video' && clip.transform.crop === 'fit'
    ? `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=decrease,pad=${composition.resolution.width}:${composition.resolution.height}:(ow-iw)/2:(oh-ih)/2`
    : `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=increase,crop=${composition.resolution.width}:${composition.resolution.height}`;
  return `[${index}:v]${scaleAndCrop},setsar=1,fps=${composition.fps},trim=duration=${clipDurationSeconds(clip)},setpts=PTS-STARTPTS[v${index}]`;
}

function sourceAudioFilter(clip: TimelineClip, index: number, composition: CompositionSpec): string {
  const volume = ffmpegNumber(clip.volume * composition.audio.original_volume);
  return `[${index}:a]atrim=duration=${clipDurationSeconds(clip)},asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=${volume}[a${index}]`;
}

function silentAudioFilter(clip: TimelineClip, index: number): string {
  return `anullsrc=channel_layout=stereo:sample_rate=48000,atrim=duration=${clipDurationSeconds(clip)},asetpts=PTS-STARTPTS,aformat=sample_fmts=fltp:channel_layouts=stereo[a${index}]`;
}

function photoVideoFilter(clip: PhotoTimelineClip, index: number, composition: CompositionSpec): string {
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
  return `[${index}:v]scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=increase,crop=${composition.resolution.width}:${composition.resolution.height},setsar=1,fps=${composition.fps},zoompan=z='${zoom}':x='${x}':y='${y}':d=1:s=${composition.resolution.width}x${composition.resolution.height}:fps=${composition.fps},trim=duration=${clipDurationSeconds(clip)},setpts=PTS-STARTPTS[v${index}]`;
}

function clipDurationSeconds(clip: {duration_ms: number}): string {
  return Math.max(0.001, clip.duration_ms / 1000).toFixed(3);
}

function ffmpegNumber(value: number): string {
  return Number.isFinite(value) ? value.toFixed(4) : '0.0000';
}

function normalizeCaptionText(value: string): string {
  return wrapCaptionText(value.trim().replace(/\s+/g, ' ').slice(0, 120), 32, 3);
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
    for (const [index, caption] of composition.captions.entries()) {
      const text = normalizeCaptionText(caption.text);
      if (!text) {
        continue;
      }
      const timing = captionTiming(caption, composition);
      if (!timing) {
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
