import {spawn} from 'node:child_process';
import {mkdir, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import type {CompositionSpec} from '../types.js';
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

function concatVideoFilter(composition: CompositionSpec): string {
  const filters = composition.clips.map((clip, index) => {
    const scaleAndCrop = clip.transform.kind === 'video' && clip.transform.crop === 'fit'
      ? `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=decrease,pad=${composition.resolution.width}:${composition.resolution.height}:(ow-iw)/2:(oh-ih)/2`
      : `scale=${composition.resolution.width}:${composition.resolution.height}:force_original_aspect_ratio=increase,crop=${composition.resolution.width}:${composition.resolution.height}`;
    return `[${index}:v]${scaleAndCrop},setsar=1,fps=${composition.fps},trim=duration=${clipDurationSeconds(clip)},setpts=PTS-STARTPTS[v${index}]`;
  });
  const inputs = composition.clips.map((_, index) => `[v${index}]`).join('');
  return [...filters, `${inputs}concat=n=${composition.clips.length}:v=1:a=0,format=yuv420p[outv]`].join(';');
}

function clipDurationSeconds(clip: {duration_ms: number}): string {
  return Math.max(0.001, clip.duration_ms / 1000).toFixed(3);
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
}

export class FfmpegCompositionRenderer implements RemotionRenderer {
  private readonly ffmpegBinary: string;

  constructor(options: FfmpegCompositionRendererOptions = {}) {
    this.ffmpegBinary = options.ffmpegBinary ?? 'ffmpeg';
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
      const inputPath = timelineClip.media_type === 'video' ? timelineClip.prepared_path : timelineClip.source_path;
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
}
