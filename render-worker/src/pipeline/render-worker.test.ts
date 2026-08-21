import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp, readFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {DeterministicFixtureFfmpegClient} from '../ffmpeg/client.js';
import {FixtureRemotionRenderer} from '../remotion/renderer.js';
import {LocalFixtureObjectStore} from '../storage/object-store.js';
import {canonicalJson, resolutionForAspectRatio} from '../determinism.js';
import {RenderAssetError, RenderInputError} from './errors.js';
import {DeterministicRenderWorker} from './render-worker.js';
import {buildComposition} from './timeline.js';
import {fixtureMediaAssets, fixtureRenderInput, fixtureStoryPlan} from '../__fixtures__/story-plan.js';

async function createHarness() {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-render-test-'));
  const objectStore = new LocalFixtureObjectStore(path.join(root, 'object-store'));
  await objectStore.putText('source/photo-1.jpg', 'fixture photo bytes');
  await objectStore.putText('source/video-1.mov', 'fixture video bytes');
  await objectStore.putText('music/warm-theme.m4a', 'fixture music bytes');
  const ffmpeg = new DeterministicFixtureFfmpegClient();
  const remotion = new FixtureRemotionRenderer();
  const worker = new DeterministicRenderWorker({
    objectStore,
    ffmpeg,
    remotion,
    workRoot: path.join(root, 'work'),
    now: () => new Date('2026-08-21T12:00:00.000Z'),
  });
  return {root, objectStore, ffmpeg, remotion, worker};
}

test('renders a validated story plan deterministically', async () => {
  const harness = await createHarness();
  const input = fixtureRenderInput();

  const first = await harness.worker.render(input);
  const second = await harness.worker.render(input);

  assert.equal(canonicalJson(first.composition), canonicalJson(second.composition));
  assert.deepEqual(first.record, second.record);
  assert.equal(first.record.status, 'succeeded');
  assert.deepEqual(first.record.output_resolution, {width: 1080, height: 1920});
  assert.equal(await harness.objectStore.objectExists(input.output_s3_key), true);

  const output = await readFile(harness.objectStore.localPath(input.output_s3_key), 'utf8');
  assert.match(output, /fixture mp4/);
});

test('preprocesses video clips with exact source timestamps and audio settings', async () => {
  const harness = await createHarness();
  await harness.worker.render(fixtureRenderInput());

  const trimOperations = harness.ffmpeg.operations.filter((operation) => operation.name === 'trimVideo');
  assert.equal(trimOperations.length, 1);
  const trim = trimOperations[0];
  assert.equal(trim.request.startMicros, 1_250_000);
  assert.equal(trim.request.endMicros, 5_750_000);
  assert.equal(trim.request.speed, 1.25);
  assert.equal(trim.request.keepAudio, true);

  const renderedClip = harness.remotion.rendered[0].clips.find((clip) => clip.asset_id === 'video-1');
  assert.ok(renderedClip);
  assert.equal(renderedClip.prepared_path, trim.request.outputPath);
  assert.equal(renderedClip.transition, 'fade');
  assert.equal(renderedClip.volume, 0.72);
});

test('normalizes transitions and resolves audio deterministically', () => {
  const plan = fixtureStoryPlan();
  const assets = fixtureMediaAssets();
  const assetPaths = new Map(assets.map((asset) => [asset.id, `/asset/${asset.id}`]));
  const composition = buildComposition(plan, assets, assetPaths, new Map(), {
    work_dir: '/work',
    assets_dir: '/work/assets',
    clips_dir: '/work/clips',
    output_file: '/work/story.mp4',
    composition_file: '/work/composition.json',
  });

  assert.equal(composition.clips[0].transition, 'cut');
  assert.equal(composition.clips[1].transition, 'fade');
  assert.equal(composition.audio.background_path, '/work/assets/background-audio');
  assert.equal(composition.audio.fade_in_frames, 18);
  assert.equal(composition.audio.fade_out_frames, 27);
});

test('supports the required output aspect ratios', () => {
  assert.deepEqual(resolutionForAspectRatio('9:16'), {width: 1080, height: 1920});
  assert.deepEqual(resolutionForAspectRatio('1:1'), {width: 1080, height: 1080});
  assert.deepEqual(resolutionForAspectRatio('16:9'), {width: 1920, height: 1080});
});

test('fails before rendering when source media is missing', async () => {
  const harness = await createHarness();
  const input = fixtureRenderInput({
    media_assets: [{...fixtureMediaAssets()[0], s3_key: 'source/missing.jpg'}],
    story_plan: fixtureStoryPlan({moments: [fixtureStoryPlan().moments[0]]}),
  });

  await assert.rejects(() => harness.worker.render(input), RenderAssetError);
  assert.equal(harness.remotion.rendered.length, 0);
});

test('rejects unsupported aspect ratios and duplicate assets', async () => {
  const harness = await createHarness();
  await assert.rejects(
    () => harness.worker.render(fixtureRenderInput({story_plan: fixtureStoryPlan({output: {aspect_ratio: '4:5', max_duration_seconds: 15, preset: 'social'}})})),
    RenderInputError,
  );
  await assert.rejects(
    () => harness.worker.render(fixtureRenderInput({media_assets: [fixtureMediaAssets()[0], fixtureMediaAssets()[0]]})),
    RenderInputError,
  );
});
