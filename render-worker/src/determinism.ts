import {createHash} from 'node:crypto';
import {mkdir} from 'node:fs/promises';
import path from 'node:path';
import type {AspectRatio, RenderInput, Resolution, StoryBRoll, StoryMoment} from './types.js';

export const DEFAULT_FPS = 30;

export function canonicalJson(value: unknown): string {
  if (value === null || typeof value !== 'object') {
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map((item) => canonicalJson(item)).join(',')}]`;
  }
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, entryValue]) => entryValue !== undefined)
    .sort(([left], [right]) => left.localeCompare(right));
  return `{${entries.map(([key, entryValue]) => `${JSON.stringify(key)}:${canonicalJson(entryValue)}`).join(',')}}`;
}

export function stableHash(value: unknown): string {
  return createHash('sha256').update(canonicalJson(value)).digest('hex');
}

export function renderFingerprint(input: RenderInput, rendererVersion: string): string {
  return stableHash({
    rendererVersion,
    projectType: input.project_type ?? input.story_plan.project_type ?? 'story',
    projectConfiguration: input.project_configuration ?? {},
    storyPlan: input.story_plan,
    mediaAssets: input.media_assets,
  });
}

export function resolutionForAspectRatio(aspectRatio: AspectRatio | string): Resolution {
  switch (aspectRatio) {
    case '9:16':
      return {width: 1080, height: 1920};
    case '1:1':
      return {width: 1080, height: 1080};
    case '16:9':
      return {width: 1920, height: 1080};
    default:
      throw new Error(`unsupported aspect ratio: ${aspectRatio}`);
  }
}

export function framesFromMilliseconds(milliseconds: number, fps = DEFAULT_FPS): number {
  return Math.max(1, Math.round((milliseconds / 1000) * fps));
}

export function millisecondsFromFrames(frames: number, fps = DEFAULT_FPS): number {
  return Math.round((frames / fps) * 1000);
}

export function stableClipId(moment: StoryMoment, index: number): string {
  return stableHash({index, moment}).slice(0, 20);
}

export function stableBRollClipId(overlay: StoryBRoll, index: number): string {
  return stableHash({index, overlay}).slice(0, 20);
}

export function deterministicUnit(seed: string, salt: string): number {
  const hash = stableHash({seed, salt}).slice(0, 12);
  return Number.parseInt(hash, 16) / 0xffffffffffff;
}

export async function ensureRenderDirs(baseDir: string, fingerprint: string) {
  const workDir = path.join(baseDir, fingerprint.slice(0, 24));
  const paths = {
    work_dir: workDir,
    assets_dir: path.join(workDir, 'assets'),
    clips_dir: path.join(workDir, 'clips'),
    output_file: path.join(workDir, 'story.mp4'),
    composition_file: path.join(workDir, 'composition.json'),
  };
  await Promise.all([mkdir(paths.assets_dir, {recursive: true}), mkdir(paths.clips_dir, {recursive: true})]);
  return paths;
}
