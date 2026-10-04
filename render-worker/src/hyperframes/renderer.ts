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
  browserPath?: string;
}

export class HyperframesCliRenderer implements HyperframesRenderer {
  private readonly timeoutMs: number;

  constructor(private readonly options: HyperframesCliRendererOptions) {
    this.timeoutMs = options.timeoutMs ?? 5 * 60 * 1000;
  }

  async renderOverlay(request: HyperframesOverlayRequest, outputPath: string): Promise<void> {
    await mkdir(path.dirname(outputPath), {recursive: true});
    const projectDir = path.join(path.dirname(outputPath), `${request.id}-${stableHash(request).slice(0, 12)}-project`);
    await writeHyperframesProject(request, projectDir);
    await run(this.options.binary, [
      'render',
      projectDir,
      '-c',
      'index.html',
      '-o',
      outputPath,
    ], this.timeoutMs, this.options.browserPath);
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
    const outputPath = path.join(outputDir, `${request.id}-${stableHash(request).slice(0, 12)}.webm`);
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

async function writeHyperframesProject(request: HyperframesOverlayRequest, projectDir: string): Promise<void> {
  await mkdir(projectDir, {recursive: true});
  await writeFile(path.join(projectDir, 'package.json'), canonicalJson({
    private: true,
    scripts: {render: 'hyperframes render index.html'},
    dependencies: {},
  }), 'utf8');
  await writeFile(path.join(projectDir, 'index.html'), hyperframesHtml(request), 'utf8');
}

function hyperframesHtml(request: HyperframesOverlayRequest): string {
  const payload = publicOverlayPayload(request);
  const style = payload.caption_style ?? {};
  const animationName = request.kind === 'caption_overlay' ? captionAnimationName(request.preset) : cardAnimationName(request.preset);
  const duration = Math.max(250, Math.round(request.duration_ms));
  const captionText = htmlEscape(payload.caption?.text?.trim() ?? '');
  const title = htmlEscape(payload.typography?.title?.trim() ?? '');
  const subtitle = htmlEscape(payload.typography?.subtitle?.trim() ?? '');
  const backgroundOpacity = clampNumber(style.background_opacity ?? 0.24, 0, 1);
  const background = style.background_enabled === false ? 'transparent' : `rgba(0,0,0,${backgroundOpacity.toFixed(3)})`;
  return `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=${request.width},initial-scale=1">
  <style>
    html, body {
      width: ${request.width}px;
      height: ${request.height}px;
      margin: 0;
      overflow: hidden;
      background: transparent;
    }
    body {
      font-family: Inter, Arial, sans-serif;
    }
    #root {
      position: relative;
      width: ${request.width}px;
      height: ${request.height}px;
      background: transparent;
    }
    .caption {
      position: absolute;
      left: 50%;
      bottom: ${clampNumber(style.bottom_offset ?? 170, 40, 420)}px;
      max-width: 92%;
      padding: 10px 20px;
      transform: translateX(-50%);
      opacity: 0;
      color: ${cssColor(style.text_color, '#FFD60A')};
      font-size: ${clampNumber(style.font_size ?? 72, 20, 112)}px;
      font-weight: 900;
      line-height: 1.02;
      text-align: center;
      text-transform: uppercase;
      -webkit-text-stroke: ${clampNumber(style.stroke_width ?? 4, 0, 8)}px ${cssColor(style.stroke_color, '#000000')};
      text-shadow: 0 3px 12px rgba(0,0,0,0.55);
      background: ${background};
      border-radius: 16px;
      white-space: pre-wrap;
      animation: ${animationName} ${duration}ms linear both;
    }
    .card {
      position: absolute;
      left: 50%;
      top: 50%;
      width: 78%;
      padding: 36px;
      transform: translate(-50%, -50%);
      opacity: 0;
      border-radius: 18px;
      background: rgba(0,0,0,0.58);
      color: #fff;
      text-align: center;
      box-shadow: 0 16px 48px rgba(0,0,0,0.34);
      animation: ${animationName} ${duration}ms linear both;
    }
    .card-title {
      font-size: 62px;
      font-weight: 850;
      line-height: 1.06;
    }
    .card-subtitle {
      margin-top: 16px;
      font-size: 34px;
      font-weight: 600;
      line-height: 1.18;
    }
    @keyframes wordPop {
      0% { opacity: 0; transform: translateX(-50%) scale(0.78); }
      12% { opacity: 1; transform: translateX(-50%) scale(1.18); }
      24% { opacity: 1; transform: translateX(-50%) scale(1); }
      88% { opacity: 1; transform: translateX(-50%) scale(1); }
      100% { opacity: 0; transform: translateX(-50%) scale(0.98); }
    }
    @keyframes karaokePop {
      0% { opacity: 0; transform: translateX(-50%) scale(0.92); }
      10% { opacity: 1; transform: translateX(-50%) scale(1.08); }
      22% { opacity: 1; transform: translateX(-50%) scale(1); }
      90% { opacity: 1; transform: translateX(-50%) scale(1); }
      100% { opacity: 0; transform: translateX(-50%) scale(1); }
    }
    @keyframes phrasePunch {
      0% { opacity: 0; transform: translateX(-50%) scale(1.18); filter: blur(2px); }
      14% { opacity: 1; transform: translateX(-50%) scale(1); filter: blur(0); }
      88% { opacity: 1; transform: translateX(-50%) scale(1); filter: blur(0); }
      100% { opacity: 0; transform: translateX(-50%) scale(0.98); filter: blur(0); }
    }
    @keyframes cardRise {
      0% { opacity: 0; transform: translate(-50%, -50%) translateY(34px) scale(0.94); }
      20% { opacity: 1; transform: translate(-50%, -50%) translateY(0) scale(1); }
      88% { opacity: 1; transform: translate(-50%, -50%) translateY(0) scale(1); }
      100% { opacity: 0; transform: translate(-50%, -50%) translateY(-10px) scale(1); }
    }
    @keyframes ctaPop {
      0% { opacity: 0; transform: translate(-50%, -50%) scale(0.82); }
      18% { opacity: 1; transform: translate(-50%, -50%) scale(1.06); }
      28% { opacity: 1; transform: translate(-50%, -50%) scale(1); }
      88% { opacity: 1; transform: translate(-50%, -50%) scale(1); }
      100% { opacity: 0; transform: translate(-50%, -50%) scale(0.98); }
    }
  </style>
</head>
<body>
  <div id="root">
    ${request.kind === 'caption_overlay' ? `<div class="caption">${captionText}</div>` : `<div class="card"><div class="card-title">${title}</div>${subtitle ? `<div class="card-subtitle">${subtitle}</div>` : ''}</div>`}
  </div>
</body>
</html>`;
}

function publicOverlayPayload(request: HyperframesOverlayRequest): HyperframesOverlayRequest {
  return request;
}

function captionAnimationName(preset: string): string {
  if (preset === 'karaoke_pop') {
    return 'karaokePop';
  }
  if (preset === 'phrase_punch') {
    return 'phrasePunch';
  }
  return 'wordPop';
}

function cardAnimationName(preset: string): string {
  if (preset === 'cta_pop') {
    return 'ctaPop';
  }
  return 'cardRise';
}

function cssColor(value: unknown, fallback: string): string {
  return typeof value === 'string' && /^#[0-9a-fA-F]{6}$/.test(value.trim()) ? value.trim() : fallback;
}

function htmlEscape(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function clampNumber(value: number, min: number, max: number): number {
  return Number.isFinite(value) ? Math.max(min, Math.min(max, value)) : min;
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

async function run(command: string, args: string[], timeoutMs: number, browserPath?: string): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const env = browserPath ? {...process.env, HYPERFRAMES_BROWSER_PATH: browserPath} : process.env;
    const child = spawn(command, args, {stdio: ['ignore', 'pipe', 'pipe'], env});
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
