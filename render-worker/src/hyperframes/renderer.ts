import {spawn} from 'node:child_process';
import {mkdir, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {canonicalJson, stableHash} from '../determinism.js';
import type {
  CaptionStyle,
  CompositionSpec,
  HyperframesOverlayMetadata,
  Resolution,
  StoryCaption,
  TextOverlay,
} from '../types.js';

export interface HyperframesOverlayRequest {
  id: string;
  kind: 'caption_overlay' | 'card_overlay';
  preset: string;
  width: number;
  height: number;
  fps: number;
  start_ms: number;
  duration_ms: number;
  caption?: StoryCaption;
  caption_style?: CaptionStyle;
  typography?: TextOverlay;
}

export interface HyperframesOverlayResult {
  request: HyperframesOverlayRequest;
  outputPath: string;
}

export interface HyperframesRenderer {
  renderOverlay(request: HyperframesOverlayRequest, outputPath: string): Promise<void>;
}

export interface HyperframesOverlayPlan {
  request_count: number;
  caption_request_count: number;
  card_request_count: number;
  presets: string;
}

export class DisabledHyperframesRenderer implements HyperframesRenderer {
  async renderOverlay(): Promise<void> {
    throw new Error('HyperFrames renderer is disabled');
  }
}

export class FixtureHyperframesRenderer implements HyperframesRenderer {
  readonly rendered: HyperframesOverlayRequest[] = [];

  async renderOverlay(request: HyperframesOverlayRequest, outputPath: string): Promise<void> {
    this.rendered.push(request);
    await mkdir(path.dirname(outputPath), {recursive: true});
    await writeFile(outputPath, `fixture hyperframes overlay\n${canonicalJson(request)}\n`, 'utf8');
  }
}

export interface HyperframesCliRendererOptions {
  binary: string;
  timeoutMs?: number;
}

export class HyperframesCliRenderer implements HyperframesRenderer {
  private readonly timeoutMs: number;

  constructor(private readonly options: HyperframesCliRendererOptions) {
    this.timeoutMs = options.timeoutMs ?? 5 * 60 * 1000;
  }

  async renderOverlay(request: HyperframesOverlayRequest, outputPath: string): Promise<void> {
    await mkdir(path.dirname(outputPath), {recursive: true});
    const requestPath = path.join(path.dirname(outputPath), `${request.id}-${stableHash(request).slice(0, 12)}.hyperframes.json`);
    await writeFile(requestPath, canonicalJson(request), 'utf8');
    await run(this.options.binary, ['render', '--input', requestPath, '--output', outputPath], this.timeoutMs);
  }
}

export async function renderHyperframesOverlays(
  composition: CompositionSpec,
  renderer: HyperframesRenderer,
  outputDir: string,
): Promise<HyperframesOverlayResult[]> {
  const requests = hyperframesOverlayRequests(composition);
  const results: HyperframesOverlayResult[] = [];
  if (requests.length === 0) {
    return results;
  }
  await mkdir(outputDir, {recursive: true});
  for (const request of requests) {
    const outputPath = path.join(outputDir, `${request.id}-${stableHash(request).slice(0, 12)}.mp4`);
    await renderer.renderOverlay(request, outputPath);
    results.push({request, outputPath});
  }
  return results;
}

export function applyHyperframesOverlays(
  composition: CompositionSpec,
  overlays: HyperframesOverlayResult[],
): CompositionSpec {
  if (overlays.length === 0) {
    return composition;
  }
  return {
    ...composition,
    broll: [
      ...composition.broll,
      ...overlays.map((overlay) => hyperframesOverlayToBRoll(composition, overlay)),
    ],
    captions: composition.captions.filter((caption) => !overlays.some((overlay) => overlay.request.kind === 'caption_overlay' && overlay.request.caption?.moment_id === caption.moment_id && overlay.request.caption?.start_ms === caption.start_ms && overlay.request.caption?.end_ms === caption.end_ms && overlay.request.caption?.text === caption.text)),
    hyperframes: {
      enabled: true,
      overlays: overlays.map((overlay) => hyperframesOverlayMetadata(overlay)),
    },
  };
}

export function hyperframesOverlayRequests(composition: CompositionSpec): HyperframesOverlayRequest[] {
  const requests: HyperframesOverlayRequest[] = [];
  const captionMotion = normalizedCaptionMotionPreset(composition.caption_style?.motion_preset);
  if (captionMotion) {
    for (const [index, caption] of composition.captions.entries()) {
      const timing = captionTiming(caption, composition);
      if (!timing) {
        continue;
      }
      requests.push({
        id: `hf-caption-${index}`,
        kind: 'caption_overlay',
        preset: captionMotion,
        width: composition.resolution.width,
        height: composition.resolution.height,
        fps: composition.fps,
        start_ms: timing.startMs,
        duration_ms: timing.endMs - timing.startMs,
        caption,
        caption_style: composition.caption_style,
      });
    }
  }
  for (const clip of composition.clips) {
    for (const [index, overlay] of (clip.overlays ?? []).entries()) {
      const preset = normalizedCardMotionPreset(overlay.animation);
      if (!preset) {
        continue;
      }
      const clipStartMs = Math.round((clip.start_frame / composition.fps) * 1000);
      const startMs = clamp(clipStartMs + Math.round(overlay.start_ms ?? 0), clipStartMs, clipStartMs + clip.duration_ms);
      const durationMs = clamp(Math.round(overlay.duration_ms ?? 0), 250, Math.max(250, clipStartMs + clip.duration_ms - startMs));
      requests.push({
        id: `hf-card-${clip.clip_id}-${index}`,
        kind: 'card_overlay',
        preset,
        width: composition.resolution.width,
        height: composition.resolution.height,
        fps: composition.fps,
        start_ms: startMs,
        duration_ms: durationMs,
        typography: overlay,
      });
    }
  }
  return requests.slice(0, 12);
}

export function hyperframesOverlayPlan(composition: CompositionSpec): HyperframesOverlayPlan {
  const requests = hyperframesOverlayRequests(composition);
  return {
    request_count: requests.length,
    caption_request_count: requests.filter((request) => request.kind === 'caption_overlay').length,
    card_request_count: requests.filter((request) => request.kind === 'card_overlay').length,
    presets: requests.map((request) => `${request.kind}:${request.preset}`).join(','),
  };
}

function hyperframesOverlayToBRoll(composition: CompositionSpec, overlay: HyperframesOverlayResult): CompositionSpec['broll'][number] {
  const durationFrames = Math.max(1, Math.round((overlay.request.duration_ms / 1000) * composition.fps));
  const startFrame = Math.max(0, Math.round((overlay.request.start_ms / 1000) * composition.fps));
  return {
    broll_id: overlay.request.id,
    asset_id: overlay.request.id,
    media_type: 'video',
    source_path: overlay.outputPath,
    prepared_path: overlay.outputPath,
    start_frame: startFrame,
    duration_frames: durationFrames,
    duration_ms: Math.round((durationFrames / composition.fps) * 1000),
    crop_intent: 'fit',
    transform: {kind: 'video', crop: 'fit', scale: 1},
    presentation: 'full_frame',
    placement: 'center',
    size: 'large',
    opacity: 1,
    frame: 'none',
    animation: 'none',
    z_index: 8,
    generated_by: 'hyperframes',
  };
}

function hyperframesOverlayMetadata(overlay: HyperframesOverlayResult): HyperframesOverlayMetadata {
  return {
    id: overlay.request.id,
    kind: overlay.request.kind,
    preset: overlay.request.preset,
    start_ms: overlay.request.start_ms,
    duration_ms: overlay.request.duration_ms,
    output_path: overlay.outputPath,
  };
}

function normalizedCaptionMotionPreset(value: unknown): string | undefined {
  if (typeof value !== 'string') {
    return undefined;
  }
  const normalized = value.trim().toLowerCase().replace(/[-\s]+/g, '_');
  return ['word_pop', 'karaoke_pop', 'phrase_punch'].includes(normalized) ? normalized : undefined;
}

function normalizedCardMotionPreset(value: unknown): string | undefined {
  if (typeof value !== 'string') {
    return undefined;
  }
  const normalized = value.trim().toLowerCase().replace(/[-\s]+/g, '_');
  return ['card_rise', 'headline_reveal', 'cta_pop', 'feature_card'].includes(normalized) ? normalized : undefined;
}

function captionTiming(caption: StoryCaption, composition: {duration_ms: number; clips: CompositionSpec['clips']; fps: number}): {startMs: number; endMs: number} | undefined {
  const fallbackClip = composition.clips.find((clip) => clip.scene_id === caption.moment_id || clip.clip_id === caption.moment_id || clip.asset_id === caption.moment_id);
  const fallbackStartMs = fallbackClip ? Math.round((fallbackClip.start_frame / composition.fps) * 1000) : 0;
  const fallbackEndMs = fallbackClip ? fallbackStartMs + fallbackClip.duration_ms : composition.duration_ms;
  const startMs = clamp(caption.start_ms ?? fallbackStartMs, 0, composition.duration_ms);
  const endMs = clamp(caption.end_ms ?? fallbackEndMs, startMs, composition.duration_ms);
  if (endMs - startMs < 250) {
    return undefined;
  }
  return {startMs, endMs};
}

function clamp(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) {
    return min;
  }
  return Math.max(min, Math.min(max, value));
}

async function run(command: string, args: string[], timeoutMs: number): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, args, {stdio: ['ignore', 'pipe', 'pipe']});
    let stderr = '';
    let settled = false;
    const timeout = setTimeout(() => {
      if (settled) {
        return;
      }
      settled = true;
      child.kill('SIGKILL');
      reject(new Error(`${command} timed out after ${timeoutMs}ms: ${stderr}`));
    }, timeoutMs);
    child.stderr.on('data', (chunk) => {
      stderr += String(chunk);
      if (stderr.length > 16000) {
        stderr = stderr.slice(-16000);
      }
    });
    child.on('error', (error) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timeout);
      reject(error);
    });
    child.on('close', (code) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timeout);
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} exited with ${code}: ${stderr}`));
    });
  });
}
