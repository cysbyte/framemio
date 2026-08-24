import {fileURLToPath} from 'node:url';
import path from 'node:path';
import {runRenderWorker} from './runner.js';
export {DeterministicRenderWorker} from './pipeline/render-worker.js';
export {RenderAssetError, RenderInputError} from './pipeline/errors.js';
export {buildComposition} from './pipeline/timeline.js';
export {ShellFfmpegClient, DeterministicFixtureFfmpegClient} from './ffmpeg/client.js';
export {FixtureRemotionRenderer, RemotionCliRenderer, FfmpegCompositionRenderer} from './remotion/renderer.js';
export {AwsCliObjectStore, LocalFixtureObjectStore} from './storage/object-store.js';
export {runRenderWorker, loadConfig} from './runner.js';
export {RENDER_VERSION} from './types.js';
export type {
  CompositionSpec,
  RenderInput,
  RenderRecord,
  RenderResult,
  SourceMediaAsset,
  StoryPlan,
  TimelineClip,
} from './types.js';

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  runRenderWorker().catch((error: unknown) => {
    console.error(JSON.stringify({level: 'error', msg: 'render worker stopped', error: error instanceof Error ? error.message : String(error)}));
    process.exitCode = 1;
  });
}
