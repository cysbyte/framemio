import {mkdir, rename, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {DEFAULT_FPS, ensureRenderDirs, renderFingerprint, stableBRollClipId, stableClipId} from '../determinism.js';
import type {FfmpegClient} from '../ffmpeg/client.js';
import {applyHyperframesOverlays, renderHyperframesOverlays} from '../hyperframes/renderer.js';
import type {HyperframesRenderer} from '../hyperframes/renderer.js';
import type {RemotionRenderer} from '../remotion/renderer.js';
import type {ObjectStore} from '../storage/object-store.js';
import type {CompositionSpec, RenderInput, RenderRecord, RenderResult, SourceMediaAsset} from '../types.js';
import {RENDER_VERSION} from '../types.js';
import {RenderAssetError, RenderInputError} from './errors.js';
import {buildComposition, mediaExtension} from './timeline.js';

export interface RenderWorkerDependencies {
  objectStore: ObjectStore;
  ffmpeg: FfmpegClient;
  remotion: RemotionRenderer;
  hyperframes?: HyperframesRenderer;
  workRoot: string;
  compositionOutputIsFinal?: boolean;
  now?: () => Date;
}

export class DeterministicRenderWorker {
  constructor(private readonly dependencies: RenderWorkerDependencies) {}

  async render(input: RenderInput): Promise<RenderResult> {
    const rendererVersion = input.renderer_version ?? RENDER_VERSION;
    console.info(JSON.stringify({
      level: 'info',
      msg: 'render job started',
      job_id: input.job_id,
      project_id: input.project_id,
      media_count: input.media_assets.length,
      moment_count: input.story_plan.moments.length,
      broll_count: input.story_plan.broll?.length ?? 0,
      broll_trace: brollTrace(input.story_plan.broll ?? []),
      caption_count: input.story_plan.captions?.length ?? 0,
      caption_style_preset: input.story_plan.caption_style?.preset ?? 'unset',
      caption_style_presentation: input.story_plan.caption_style?.presentation ?? 'unset',
      audio_source_requested: input.story_plan.audio?.source ?? 'unset',
      audio_original_audio_requested: input.story_plan.audio?.original_audio ?? null,
      audio_original_volume_requested: input.story_plan.audio?.original_volume ?? null,
      output_s3_key: input.output_s3_key,
      renderer_version: rendererVersion,
    }));
    validateInput(input, rendererVersion);
    const fingerprint = renderFingerprint(input, rendererVersion);
    const paths = await ensureRenderDirs(this.dependencies.workRoot, fingerprint);
    console.info(JSON.stringify({level: 'info', msg: 'render workspace ready', job_id: input.job_id, project_id: input.project_id, fingerprint, work_dir: paths.work_dir}));
    const assetsByID = indexAssets(input.media_assets);
    const assetPaths = await this.downloadRequiredAssets(input, assetsByID, paths.assets_dir);
    console.info(JSON.stringify({level: 'info', msg: 'render source assets downloaded', job_id: input.job_id, project_id: input.project_id, downloaded_count: assetPaths.size}));
    const preparedPaths = await this.prepareClips(input, assetsByID, assetPaths, paths.clips_dir);
    console.info(JSON.stringify({level: 'info', msg: 'render clips prepared', job_id: input.job_id, project_id: input.project_id, prepared_count: preparedPaths.size}));
    let composition = buildComposition(input.story_plan, input.media_assets, assetPaths, preparedPaths, paths, {rendererVersion, fps: DEFAULT_FPS, projectType: input.project_type, projectConfiguration: input.project_configuration});
    if (this.dependencies.hyperframes) {
      const hyperframesStartedAt = Date.now();
      const overlays = await renderHyperframesOverlays(composition, this.dependencies.hyperframes, path.join(paths.work_dir, 'hyperframes'));
      composition = applyHyperframesOverlays(composition, overlays);
      console.info(JSON.stringify({
        level: 'info',
        msg: 'hyperframes overlays prepared',
        job_id: input.job_id,
        project_id: input.project_id,
        overlay_count: overlays.length,
        presets: overlays.map((overlay) => `${overlay.request.kind}:${overlay.request.preset}`).join(','),
        duration_ms: Date.now() - hyperframesStartedAt,
      }));
    }
    await writeFile(paths.composition_file, JSON.stringify(composition, null, 2), 'utf8');
    console.info(JSON.stringify({
      level: 'info',
      msg: 'render composition built',
      job_id: input.job_id,
      project_id: input.project_id,
      duration_ms: composition.duration_ms,
      width: composition.resolution.width,
      height: composition.resolution.height,
      clip_count: composition.clips.length,
      video_clip_count: composition.clips.filter((clip) => clip.media_type === 'video').length,
      audio_eligible_video_clip_count: composition.clips.filter((clip) => clip.media_type === 'video' && clip.volume > 0).length,
      broll_count: composition.broll.length,
      broll_trace: timelineBRollTrace(composition.broll),
      caption_count: composition.captions.length,
      hyperframes_overlay_count: composition.hyperframes?.overlays.length ?? 0,
      caption_style_preset: composition.caption_style?.preset ?? 'unset',
      caption_style_presentation: composition.caption_style?.presentation ?? 'unset',
      caption_style_motion_preset: composition.caption_style?.motion_preset ?? 'unset',
      audio_source_resolved: composition.audio.source,
      audio_original_audio_resolved: composition.audio.original_audio,
      audio_original_volume_resolved: composition.audio.original_volume,
      transition_summary: transitionSummary(composition),
    }));
    const remotionIntermediate = path.join(paths.work_dir, 'remotion-output.mp4');
    await this.dependencies.remotion.renderComposition(composition, remotionIntermediate);
    console.info(JSON.stringify({level: 'info', msg: 'remotion render completed', job_id: input.job_id, project_id: input.project_id}));
    if (this.dependencies.compositionOutputIsFinal) {
      const finalizeStarted = Date.now();
      await rename(remotionIntermediate, paths.output_file);
      console.info(JSON.stringify({level: 'info', msg: 'composition output finalized without re-encode', job_id: input.job_id, project_id: input.project_id, duration_ms: Date.now() - finalizeStarted}));
    } else {
      const encodeStarted = Date.now();
      console.info(JSON.stringify({level: 'info', msg: 'ffmpeg encode started', job_id: input.job_id, project_id: input.project_id, duration_ms: composition.duration_ms, width: composition.resolution.width, height: composition.resolution.height}));
      try {
        await this.dependencies.ffmpeg.encodeMp4({
          compositionPath: paths.composition_file,
          intermediateVideoPath: remotionIntermediate,
          outputPath: paths.output_file,
          width: composition.resolution.width,
          height: composition.resolution.height,
          durationMs: composition.duration_ms,
        });
      } catch (error) {
        console.error(JSON.stringify({level: 'error', msg: 'ffmpeg encode failed', job_id: input.job_id, project_id: input.project_id, duration_ms: Date.now() - encodeStarted, error: error instanceof Error ? error.message : String(error)}));
        throw error;
      }
      console.info(JSON.stringify({level: 'info', msg: 'ffmpeg encode completed', job_id: input.job_id, project_id: input.project_id, duration_ms: Date.now() - encodeStarted}));
    }
    const uploadStarted = Date.now();
    console.info(JSON.stringify({level: 'info', msg: 'render artifact upload started', job_id: input.job_id, project_id: input.project_id, output_s3_key: input.output_s3_key}));
    await this.dependencies.objectStore.uploadObject(paths.output_file, input.output_s3_key, 'video/mp4');
    console.info(JSON.stringify({level: 'info', msg: 'render artifact uploaded', job_id: input.job_id, project_id: input.project_id, output_s3_key: input.output_s3_key, duration_ms: Date.now() - uploadStarted}));
    const now = this.nowISO();
    const record: RenderRecord = {
      job_id: input.job_id,
      project_id: input.project_id,
      story_plan_id: input.story_plan.id,
      story_plan_version: input.story_plan.version,
      render_version: rendererVersion,
      status: 'succeeded',
      output_resolution: composition.resolution,
      duration_ms: composition.duration_ms,
      s3_key: input.output_s3_key,
      content_type: 'video/mp4',
      created_at: now,
      updated_at: now,
    };
    return {record, composition};
  }

  private async downloadRequiredAssets(input: RenderInput, assetsByID: Map<string, SourceMediaAsset>, assetsDir: string): Promise<Map<string, string>> {
    await mkdir(assetsDir, {recursive: true});
    const downloaded = new Map<string, string>();
    const requiredIDs = [...new Set([
      ...input.story_plan.moments.map((moment) => moment.asset_id),
      ...(input.story_plan.broll ?? []).map((overlay) => overlay.asset_id),
    ])];
    for (const assetID of requiredIDs) {
      const asset = assetsByID.get(assetID);
      if (!asset) {
        throw new RenderInputError(`story plan references asset that was not supplied: ${assetID}`);
      }
      if (!(await this.dependencies.objectStore.objectExists(asset.s3_key))) {
        console.error(JSON.stringify({level: 'error', msg: 'render source media missing', asset_id: asset.id, s3_key: asset.s3_key}));
        throw new RenderAssetError(`required source media is missing from S3: ${asset.s3_key}`);
      }
      const destination = path.join(assetsDir, `${asset.id}${mediaExtension(asset)}`);
      console.info(JSON.stringify({level: 'info', msg: 'downloading render source media', asset_id: asset.id, media_type: asset.media_type, s3_key: asset.s3_key}));
      await this.dependencies.objectStore.downloadObject(asset.s3_key, destination);
      downloaded.set(asset.id, destination);
    }
    return downloaded;
  }

  private async prepareClips(
    input: RenderInput,
    assetsByID: Map<string, SourceMediaAsset>,
    assetPaths: Map<string, string>,
    clipsDir: string,
  ): Promise<Map<string, string>> {
    await mkdir(clipsDir, {recursive: true});
    const prepared = new Map<string, string>();
    for (const [index, moment] of input.story_plan.moments.entries()) {
      const asset = assetsByID.get(moment.asset_id);
      if (!asset) {
        throw new RenderInputError(`missing asset for moment: ${moment.asset_id}`);
      }
      if (asset.media_type !== 'video') {
        continue;
      }
      const sourcePath = assetPaths.get(asset.id);
      if (!sourcePath) {
        throw new RenderInputError(`video source was not downloaded: ${asset.id}`);
      }
      const startMicros = moment.start_micros ?? 0;
      const endMicros = moment.end_micros ?? Math.round((asset.duration_seconds ?? moment.duration_ms / 1000) * 1_000_000);
      const clipID = stableClipId(moment, index);
      const destination = path.join(clipsDir, `${clipID}.mp4`);
      const keepAudio = input.story_plan.audio?.original_audio ?? true;
      const sourceProbe = await this.dependencies.ffmpeg.probeMedia(sourcePath);
      console.info(JSON.stringify({
        level: 'info',
        msg: 'preparing render video clip',
        job_id: input.job_id,
        project_id: input.project_id,
        clip_id: clipID,
        asset_id: asset.id,
        moment_id: moment.moment_id ?? '',
        source_start_micros: startMicros,
        source_end_micros: endMicros,
        speed: moment.speed ?? 1,
        keep_audio: keepAudio,
        audio_source_requested: input.story_plan.audio?.source ?? 'unset',
        source_audio_stream_count: sourceProbe.audioStreamCount,
        source_video_stream_count: sourceProbe.videoStreamCount,
        source_duration_seconds: sourceProbe.durationSeconds ?? null,
      }));
      await this.dependencies.ffmpeg.trimVideo({
        inputPath: sourcePath,
        outputPath: destination,
        startMicros,
        endMicros,
        speed: moment.speed ?? 1,
        keepAudio,
      });
      const preparedProbe = await this.dependencies.ffmpeg.probeMedia(destination);
      console.info(JSON.stringify({
        level: 'info',
        msg: 'prepared render video clip',
        job_id: input.job_id,
        project_id: input.project_id,
        clip_id: clipID,
        asset_id: asset.id,
        keep_audio: keepAudio,
        prepared_audio_stream_count: preparedProbe.audioStreamCount,
        prepared_video_stream_count: preparedProbe.videoStreamCount,
        prepared_duration_seconds: preparedProbe.durationSeconds ?? null,
      }));
      prepared.set(clipID, destination);
    }
    for (const [index, overlay] of (input.story_plan.broll ?? []).entries()) {
      const asset = assetsByID.get(overlay.asset_id);
      if (!asset) {
        throw new RenderInputError(`missing asset for b-roll: ${overlay.asset_id}`);
      }
      if (asset.media_type !== 'video') {
        continue;
      }
      const sourcePath = assetPaths.get(asset.id);
      if (!sourcePath) {
        throw new RenderInputError(`b-roll video source was not downloaded: ${asset.id}`);
      }
      const startMicros = overlay.source_start_micros ?? 0;
      const endMicros = overlay.source_end_micros ?? Math.round((asset.duration_seconds ?? overlay.duration_ms / 1000) * 1_000_000);
      const clipID = stableBRollClipId(overlay, index);
      const destination = path.join(clipsDir, `${clipID}.mp4`);
      await this.dependencies.ffmpeg.trimVideo({
        inputPath: sourcePath,
        outputPath: destination,
        startMicros,
        endMicros,
        speed: 1,
        keepAudio: false,
      });
      prepared.set(clipID, destination);
    }
    return prepared;
  }

  private nowISO(): string {
    return (this.dependencies.now?.() ?? new Date()).toISOString();
  }
}

function transitionSummary(composition: CompositionSpec): string {
  return composition.clips.map((clip, index) => `${index + 1}:${clip.asset_id}:${clip.transition}`).join(',');
}

export function brollTrace(overlays: NonNullable<RenderInput['story_plan']['broll']>): string {
  return overlays.map((overlay, index) => {
    const id = overlay.broll_id?.trim() || `broll_index_${index}`;
    const assetID = overlay.asset_id?.trim() || overlay.media_id?.trim() || 'unset';
    const score = typeof overlay.score === 'number' && Number.isFinite(overlay.score) ? overlay.score.toFixed(3) : 'unset';
    const confidence = typeof overlay.confidence === 'number' && Number.isFinite(overlay.confidence) ? overlay.confidence.toFixed(3) : 'unset';
    return `${id}:${assetID}:start=${overlay.start_ms}:duration=${overlay.duration_ms}:score=${score}:confidence=${confidence}`;
  }).sort().join(',');
}

function timelineBRollTrace(overlays: CompositionSpec['broll']): string {
  return overlays.map((overlay) => `${overlay.broll_id}:${overlay.asset_id}:start_frame=${overlay.start_frame}:duration_frames=${overlay.duration_frames}`).sort().join(',');
}

function validateInput(input: RenderInput, rendererVersion: string): void {
  if (!input.job_id || !input.project_id || !input.output_s3_key) {
    throw new RenderInputError('job_id, project_id, and output_s3_key are required');
  }
  if (!rendererVersion) {
    throw new RenderInputError('renderer version is required');
  }
  if (!input.story_plan || input.story_plan.project_id !== input.project_id) {
    throw new RenderInputError('validated story plan must belong to the render project');
  }
  if (input.project_type && !['story', 'promo', 'reel'].includes(input.project_type)) {
    throw new RenderInputError(`unsupported project type: ${input.project_type}`);
  }
  if (input.story_plan.project_type && input.project_type && input.story_plan.project_type !== input.project_type) {
    throw new RenderInputError('render project type does not match the edit plan');
  }
  if (!['9:16', '1:1', '16:9'].includes(input.story_plan.output.aspect_ratio)) {
    throw new RenderInputError(`unsupported render aspect ratio: ${input.story_plan.output.aspect_ratio}`);
  }
  const resolution = resolutionForValidatedAspectRatio(input.story_plan.output.aspect_ratio);
  if (resolution.width <= 0 || resolution.height <= 0 || resolution.width > 3840 || resolution.height > 3840) {
    throw new RenderInputError(`unsupported output dimensions: ${resolution.width}x${resolution.height}`);
  }
  if ((input.project_type === 'reel' || input.story_plan.project_type === 'reel') && input.story_plan.output.aspect_ratio !== '9:16') {
    throw new RenderInputError('reel renders must use 9:16 aspect ratio');
  }
  if (input.story_plan.moments.length === 0) {
    throw new RenderInputError('story plan must include at least one moment');
  }
  const assetsByID = new Map(input.media_assets.map((asset) => [asset.id, asset]));
  let totalDurationMs = 0;
  for (const [index, moment] of input.story_plan.moments.entries()) {
    const asset = moment.asset_id ? assetsByID.get(moment.asset_id) : undefined;
    if (!moment.asset_id || !asset) {
      throw new RenderInputError(`missing media asset for scene at index ${index}: ${moment.asset_id || 'unset'}`);
    }
    if (!Number.isFinite(moment.duration_ms) || moment.duration_ms <= 0) {
      throw new RenderInputError(`invalid scene duration at index ${index}`);
    }
    totalDurationMs += moment.duration_ms;
    if (asset.media_type === 'video') {
      const startMicros = moment.start_micros ?? 0;
      if (startMicros < 0 || (moment.end_micros ?? 0) <= startMicros) {
        throw new RenderInputError(`invalid video source range at scene index ${index}`);
      }
    }
    validateTextOverlay(moment.typography, `moments[${index}].typography`);
    if (moment.scene_text) {
      validateTextLength(moment.scene_text.headline, 80, `moments[${index}].scene_text.headline`);
      validateTextLength(moment.scene_text.supporting, 100, `moments[${index}].scene_text.supporting`);
      validateTextLength(moment.scene_text.cta, 80, `moments[${index}].scene_text.cta`);
    }
    if (moment.voiceover?.text) {
      validateTextLength(moment.voiceover.text, 220, `moments[${index}].voiceover.text`);
    }
  }
  if (input.story_plan.total_duration_ms !== undefined && Math.abs(input.story_plan.total_duration_ms - totalDurationMs) > 34) {
    throw new RenderInputError(`story plan duration mismatch: moments=${totalDurationMs} total=${input.story_plan.total_duration_ms}`);
  }
  for (const [index, overlay] of (input.story_plan.broll ?? []).entries()) {
    const asset = overlay.asset_id ? assetsByID.get(overlay.asset_id) : undefined;
    if (!overlay.asset_id || !asset || overlay.start_ms < 0 || overlay.duration_ms <= 0) {
      throw new RenderInputError(`invalid b-roll overlay at index ${index}`);
    }
    if (overlay.start_ms + overlay.duration_ms > (input.story_plan.total_duration_ms ?? Number.MAX_SAFE_INTEGER)) {
      throw new RenderInputError(`b-roll overlay exceeds story duration at index ${index}`);
    }
    const sourceStartMicros = overlay.source_start_micros ?? 0;
    if (asset.media_type === 'video' && (sourceStartMicros < 0 || (overlay.source_end_micros ?? 0) <= sourceStartMicros)) {
      throw new RenderInputError(`invalid b-roll source segment at index ${index}`);
    }
    if (overlay.presentation && !['full_frame', 'picture_in_picture', 'split_screen', 'montage'].includes(overlay.presentation)) {
      throw new RenderInputError(`unsupported b-roll presentation at index ${index}`);
    }
    if (overlay.placement && !['top_left', 'top_right', 'bottom_left', 'bottom_right', 'center', 'left', 'right'].includes(overlay.placement)) {
      throw new RenderInputError(`unsupported b-roll placement at index ${index}`);
    }
    if (overlay.size && !['small', 'medium', 'large', 'half'].includes(overlay.size)) {
      throw new RenderInputError(`unsupported b-roll size at index ${index}`);
    }
    if (overlay.opacity !== undefined && (overlay.opacity < 0.35 || overlay.opacity > 1)) {
      throw new RenderInputError(`invalid b-roll opacity at index ${index}`);
    }
    if (overlay.frame && !['none', 'subtle', 'bold'].includes(overlay.frame)) {
      throw new RenderInputError(`unsupported b-roll frame at index ${index}`);
    }
    if (overlay.animation && !['none', 'fade', 'subtle_scale', 'scale', 'move'].includes(overlay.animation)) {
      throw new RenderInputError(`unsupported b-roll animation at index ${index}`);
    }
  }
  for (const [index, caption] of (input.story_plan.captions ?? []).entries()) {
    validateTextLength(caption.text, 220, `captions[${index}].text`);
    if ((caption.start_ms ?? 0) < 0 || (caption.end_ms ?? totalDurationMs) < (caption.start_ms ?? 0)) {
      throw new RenderInputError(`invalid caption timing at index ${index}`);
    }
  }
  if (input.story_plan.audio?.source && !['none', 'original'].includes(input.story_plan.audio.source)) {
    throw new RenderInputError(`unsupported audio source: ${input.story_plan.audio.source}`);
  }
}

function resolutionForValidatedAspectRatio(aspectRatio: string): {width: number; height: number} {
  switch (aspectRatio) {
    case '9:16':
      return {width: 1080, height: 1920};
    case '1:1':
      return {width: 1080, height: 1080};
    case '16:9':
      return {width: 1920, height: 1080};
    default:
      return {width: 0, height: 0};
  }
}

function validateTextOverlay(overlay: RenderInput['story_plan']['moments'][number]['typography'], field: string): void {
  if (!overlay) {
    return;
  }
  validateTextLength(overlay.title, 80, `${field}.title`);
  validateTextLength(overlay.subtitle, 140, `${field}.subtitle`);
  if (overlay.max_width !== undefined && (overlay.max_width < 0.2 || overlay.max_width > 0.95)) {
    throw new RenderInputError(`invalid text overlay width at ${field}`);
  }
}

function validateTextLength(value: string | undefined, maxLength: number, field: string): void {
  if (typeof value === 'string' && value.replace(/\s+/g, ' ').trim().length > maxLength) {
    throw new RenderInputError(`${field} exceeds ${maxLength} characters`);
  }
}

function indexAssets(mediaAssets: SourceMediaAsset[]): Map<string, SourceMediaAsset> {
  const assetsByID = new Map<string, SourceMediaAsset>();
  for (const asset of mediaAssets) {
    if (assetsByID.has(asset.id)) {
      throw new RenderInputError(`duplicate media asset supplied: ${asset.id}`);
    }
    assetsByID.set(asset.id, asset);
  }
  return assetsByID;
}
