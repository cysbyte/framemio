import {mkdir, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {DEFAULT_FPS, ensureRenderDirs, renderFingerprint, stableClipId} from '../determinism.js';
import type {FfmpegClient} from '../ffmpeg/client.js';
import type {RemotionRenderer} from '../remotion/renderer.js';
import type {ObjectStore} from '../storage/object-store.js';
import type {RenderInput, RenderRecord, RenderResult, SourceMediaAsset} from '../types.js';
import {RENDER_VERSION} from '../types.js';
import {RenderAssetError, RenderInputError} from './errors.js';
import {buildComposition, mediaExtension} from './timeline.js';

export interface RenderWorkerDependencies {
  objectStore: ObjectStore;
  ffmpeg: FfmpegClient;
  remotion: RemotionRenderer;
  workRoot: string;
  now?: () => Date;
}

export class DeterministicRenderWorker {
  constructor(private readonly dependencies: RenderWorkerDependencies) {}

  async render(input: RenderInput): Promise<RenderResult> {
    const rendererVersion = input.renderer_version ?? RENDER_VERSION;
    console.info(JSON.stringify({level: 'info', msg: 'render job started', job_id: input.job_id, project_id: input.project_id, media_count: input.media_assets.length, moment_count: input.story_plan.moments.length, caption_count: input.story_plan.captions?.length ?? 0, output_s3_key: input.output_s3_key, renderer_version: rendererVersion}));
    validateInput(input, rendererVersion);
    const fingerprint = renderFingerprint(input, rendererVersion);
    const paths = await ensureRenderDirs(this.dependencies.workRoot, fingerprint);
    console.info(JSON.stringify({level: 'info', msg: 'render workspace ready', job_id: input.job_id, project_id: input.project_id, fingerprint, work_dir: paths.work_dir}));
    const assetsByID = indexAssets(input.media_assets);
    const assetPaths = await this.downloadRequiredAssets(input, assetsByID, paths.assets_dir);
    console.info(JSON.stringify({level: 'info', msg: 'render source assets downloaded', job_id: input.job_id, project_id: input.project_id, downloaded_count: assetPaths.size}));
    const preparedPaths = await this.prepareClips(input, assetsByID, assetPaths, paths.clips_dir);
    console.info(JSON.stringify({level: 'info', msg: 'render clips prepared', job_id: input.job_id, project_id: input.project_id, prepared_count: preparedPaths.size}));
    const composition = buildComposition(input.story_plan, input.media_assets, assetPaths, preparedPaths, paths, {rendererVersion, fps: DEFAULT_FPS});
    await writeFile(paths.composition_file, JSON.stringify(composition, null, 2), 'utf8');
    console.info(JSON.stringify({level: 'info', msg: 'render composition built', job_id: input.job_id, project_id: input.project_id, duration_ms: composition.duration_ms, width: composition.resolution.width, height: composition.resolution.height, clip_count: composition.clips.length, caption_count: composition.captions.length}));
    const remotionIntermediate = path.join(paths.work_dir, 'remotion-output.mp4');
    await this.dependencies.remotion.renderComposition(composition, remotionIntermediate);
    console.info(JSON.stringify({level: 'info', msg: 'remotion render completed', job_id: input.job_id, project_id: input.project_id}));
    await this.dependencies.ffmpeg.encodeMp4({
      compositionPath: paths.composition_file,
      intermediateVideoPath: remotionIntermediate,
      outputPath: paths.output_file,
      width: composition.resolution.width,
      height: composition.resolution.height,
      durationMs: composition.duration_ms,
    });
    console.info(JSON.stringify({level: 'info', msg: 'ffmpeg encode completed', job_id: input.job_id, project_id: input.project_id}));
    await this.dependencies.objectStore.uploadObject(paths.output_file, input.output_s3_key, 'video/mp4');
    console.info(JSON.stringify({level: 'info', msg: 'render artifact uploaded', job_id: input.job_id, project_id: input.project_id, output_s3_key: input.output_s3_key}));
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
    const requiredIDs = [...new Set(input.story_plan.moments.map((moment) => moment.asset_id))];
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
    const backgroundKey = input.story_plan.audio?.background_s3_key;
    if (backgroundKey) {
      if (!(await this.dependencies.objectStore.objectExists(backgroundKey))) {
        throw new RenderAssetError(`required background audio is missing from S3: ${backgroundKey}`);
      }
      await this.dependencies.objectStore.downloadObject(backgroundKey, path.join(assetsDir, 'background-audio'));
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
      await this.dependencies.ffmpeg.trimVideo({
        inputPath: sourcePath,
        outputPath: destination,
        startMicros,
        endMicros,
        speed: moment.speed ?? 1,
        keepAudio: input.story_plan.audio?.original_audio ?? true,
      });
      prepared.set(clipID, destination);
    }
    return prepared;
  }

  private nowISO(): string {
    return (this.dependencies.now?.() ?? new Date()).toISOString();
  }
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
  if (!['9:16', '1:1', '16:9'].includes(input.story_plan.output.aspect_ratio)) {
    throw new RenderInputError(`unsupported render aspect ratio: ${input.story_plan.output.aspect_ratio}`);
  }
  if (input.story_plan.moments.length === 0) {
    throw new RenderInputError('story plan must include at least one moment');
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
