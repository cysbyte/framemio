import {spawn} from 'node:child_process';
import {mkdir, open, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import type {CompositionSpec, KenBurnsTransform, TimelineClip} from '../types.js';
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

function concatVideoFilter(composition: CompositionSpec): string {
  const filters = composition.clips.map((clip, index) => isPhotoTimelineClip(clip)
    ? photoVideoFilter(clip, index, composition)
    : sourceVideoFilter(clip, index, composition));
  const inputs = composition.clips.map((_, index) => `[v${index}]`).join('');
  return [...filters, `${inputs}concat=n=${composition.clips.length}:v=1:a=0,format=yuv420p[outv]`].join(';');
}

function isPhotoTimelineClip(clip: TimelineClip): clip is PhotoTimelineClip {
  return clip.transform.kind === 'photo';
}

function sourceVideoFilter(clip: TimelineClip, index: number, composition: CompositionSpec): string {
  const scaleAndCrop = clip.transform.kind === 'video' && clip.transform.crop === 'fit'
    ? `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=decrease,pad=${composition.resolution.width}:${composition.resolution.height}:(ow-iw)/2:(oh-ih)/2`
    : `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=increase,crop=${composition.resolution.width}:${composition.resolution.height}`;
  return `[${index}:v]${scaleAndCrop},setsar=1,fps=${composition.fps},trim=duration=${clipDurationSeconds(clip)},setpts=PTS-STARTPTS[v${index}]`;
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
    args.push(
      '-filter_complex',
      concatVideoFilter(composition),
      '-map',
      '[outv]',
      '-t',
      durationSeconds.toFixed(3),
      '-r',
      String(composition.fps),
      '-an',
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
