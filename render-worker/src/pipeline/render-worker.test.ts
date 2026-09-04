import test from 'node:test';
import assert from 'node:assert/strict';
import {chmod, mkdtemp, readFile, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {DeterministicFixtureFfmpegClient} from '../ffmpeg/client.js';
import {FfmpegCompositionRenderer, FixtureRemotionRenderer} from '../remotion/renderer.js';
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
  await objectStore.putText('source/photo-2.jpg', 'fixture b-roll photo bytes');
  await objectStore.putText('source/video-1.mov', 'fixture video bytes');
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

test('preprocesses b-roll video without muting A-roll audio', async () => {
  const harness = await createHarness();
  await harness.worker.render(fixtureRenderInput({
    story_plan: fixtureStoryPlan({
      broll: [{
        broll_id: 'broll-video-1',
        asset_id: 'video-1',
        media_type: 'video',
        video_analysis_version: 'video-v1',
        source_end_micros: 2_000_000,
        start_ms: 1200,
        duration_ms: 2000,
        reason: 'semantic_match',
      }],
    }),
  }));

  const trimOperations = harness.ffmpeg.operations.filter((operation) => operation.name === 'trimVideo');
  assert.equal(trimOperations.length, 2);
  assert.equal(trimOperations[0].request.keepAudio, true);
  assert.equal(trimOperations[1].request.startMicros, 0);
  assert.equal(trimOperations[1].request.endMicros, 2_000_000);
  assert.equal(trimOperations[1].request.keepAudio, false);
  assert.equal(harness.remotion.rendered[0].broll.length, 1);
});

test('ffmpeg fallback renderer overlays b-roll before captions', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-broll-render-test-'));
  const fakeFfmpeg = path.join(root, 'fake-ffmpeg.cjs');
  const argsPath = path.join(root, 'ffmpeg-args.json');
  const outputPath = path.join(root, 'story.mp4');
  await writeFile(fakeFfmpeg, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(process.env.FRAMEMIO_FFMPEG_ARGS_PATH, JSON.stringify(args));
fs.writeFileSync(args[args.length - 1], 'fake mp4');
`, 'utf8');
  await chmod(fakeFfmpeg, 0o755);

  const previousArgsPath = process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
  process.env.FRAMEMIO_FFMPEG_ARGS_PATH = argsPath;
  try {
    const assets = fixtureMediaAssets();
    const assetPaths = new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`]));
    const composition = buildComposition(fixtureStoryPlan({
      broll: [{broll_id: 'broll-photo-1', asset_id: 'photo-2', media_type: 'photo', start_ms: 1000, duration_ms: 1500, reason: 'semantic_match'}],
    }), assets, assetPaths, new Map(), {
      work_dir: root,
      assets_dir: path.join(root, 'assets'),
      clips_dir: path.join(root, 'clips'),
      output_file: outputPath,
      composition_file: path.join(root, 'composition.json'),
    });

    await new FfmpegCompositionRenderer({ffmpegBinary: fakeFfmpeg}).renderComposition(composition, outputPath);

    const args = JSON.parse(await readFile(argsPath, 'utf8')) as string[];
    const inputs = args.flatMap((arg, index) => arg === '-i' ? [args[index + 1]] : []);
    assert.deepEqual(inputs, ['/asset/IMG_1001.jpg', '/asset/IMG_1002.MOV', '/asset/IMG_1003.jpg']);
    const filterComplex = args[args.indexOf('-filter_complex') + 1];
    assert.match(filterComplex, /\[basev\]\[bv0\]overlay=0:0:enable='between\(t,1\.000,2\.500\)'\[basev_broll_0\]/);
    assert.match(filterComplex, /\[basev_broll_0\]drawtext=/);
    assert.match(filterComplex, /\[1:a\]atrim=duration=4\.500/);
  } finally {
    if (previousArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousArgsPath;
    }
  }
});

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

test('preprocesses multiple selected ranges from the same source video', async () => {
  const harness = await createHarness();
  const basePlan = fixtureStoryPlan();
  const firstVideo = {...basePlan.moments[1], moment_id: 'moment-video-1', start_micros: 4_000_000, end_micros: 8_000_000, duration_ms: 4000};
  const secondVideo = {...basePlan.moments[1], moment_id: 'moment-video-2', start_micros: 12_000_000, end_micros: 16_000_000, duration_ms: 4000};
  await harness.worker.render(fixtureRenderInput({
    story_plan: fixtureStoryPlan({moments: [basePlan.moments[0], firstVideo, secondVideo], total_duration_ms: 11_000, requested_duration_ms: 11_000}),
  }));

  const trimOperations = harness.ffmpeg.operations.filter((operation) => operation.name === 'trimVideo');
  assert.equal(trimOperations.length, 2);
  assert.equal(trimOperations[0].request.startMicros, 4_000_000);
  assert.equal(trimOperations[0].request.endMicros, 8_000_000);
  assert.equal(trimOperations[1].request.startMicros, 12_000_000);
  assert.equal(trimOperations[1].request.endMicros, 16_000_000);
  assert.notEqual(trimOperations[0].request.outputPath, trimOperations[1].request.outputPath);
});

test('applies deterministic cinematic motion to photo clips', () => {
  const plan = fixtureStoryPlan({moments: [fixtureStoryPlan().moments[0]]});
  const [photo] = fixtureMediaAssets();
  const composition = buildComposition(plan, [photo], new Map([[photo.id, '/asset/photo.jpg']]), new Map(), {
    work_dir: '/work',
    assets_dir: '/work/assets',
    clips_dir: '/work/clips',
    output_file: '/work/story.mp4',
    composition_file: '/work/composition.json',
  });

  const transform = composition.clips[0].transform;
  assert.equal(transform.kind, 'photo');
  assert.notEqual(transform.from_scale, transform.to_scale);
  assert.equal(composition.clips[0].media_type, 'photo');
});

test('normalizes typography into timeline clips', () => {
  const basePlan = fixtureStoryPlan();
  const plan = fixtureStoryPlan({
    moments: [{
      ...basePlan.moments[0],
      typography: {
        enabled: true,
        type: 'moment_title',
        title: 'A very long title that should be deterministically shortened before rendering',
        subtitle: 'Quiet light by the sea',
        style: 'neon',
        placement: 'over_face',
        animation: 'spin',
        start_ms: -50,
        duration_ms: 9000,
        max_width: 2,
        safe_area: {top: -1, right: 0.7, bottom: 0.08, left: 0.08},
        source: 'remote',
        confidence: 2,
      },
    }],
    captions: [],
    audio: {source: 'none'},
    total_duration_ms: 3000,
  });
  const [photo] = fixtureMediaAssets();
  const composition = buildComposition(plan, [photo], new Map([[photo.id, '/asset/photo.jpg']]), new Map(), {
    work_dir: '/work',
    assets_dir: '/work/assets',
    clips_dir: '/work/clips',
    output_file: '/work/story.mp4',
    composition_file: '/work/composition.json',
  });

  const typography = composition.clips[0].typography;
  assert.equal(typography?.enabled, true);
  assert.equal(typography?.style, 'cinematic');
  assert.equal(typography?.placement, 'bottom_left');
  assert.equal(typography?.animation, 'fade_up');
  assert.equal(typography?.start_ms, 0);
  assert.equal(typography?.duration_ms, 3000);
  assert.equal(typography?.max_width, 0.95);
  assert.equal(typography?.safe_area?.top, 0);
  assert.equal(typography?.safe_area?.right, 0.5);
  assert.equal(typography?.source, 'ai');
  assert.equal(typography?.confidence, 1);
  assert.equal((typography?.title ?? '').length, 40);
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

  const crossfadePlan = fixtureStoryPlan({
    moments: [
      {...fixtureStoryPlan().moments[0], transition: 'fade_in'},
      {...fixtureStoryPlan().moments[1], transition: 'crossfade'},
    ],
  });
  const crossfadeComposition = buildComposition(crossfadePlan, assets, assetPaths, new Map(), {
    work_dir: '/work',
    assets_dir: '/work/assets',
    clips_dir: '/work/clips',
    output_file: '/work/story.mp4',
    composition_file: '/work/composition.json',
  });
  assert.equal(crossfadeComposition.clips[0].transition, 'fade');
  assert.equal(crossfadeComposition.clips[1].transition, 'crossfade');
  assert.equal(composition.audio.source, 'original');
  assert.equal(composition.audio.original_audio, true);
  assert.equal(composition.audio.original_volume, 0.9);
});


test('ffmpeg fallback renderer preserves original audio without background music', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-ffmpeg-render-test-'));
  const fakeFfmpeg = path.join(root, 'fake-ffmpeg.cjs');
  const argsPath = path.join(root, 'ffmpeg-args.json');
  const outputPath = path.join(root, 'story.mp4');
  await writeFile(fakeFfmpeg, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(process.env.FRAMEMIO_FFMPEG_ARGS_PATH, JSON.stringify(args));
fs.writeFileSync(args[args.length - 1], 'fake mp4');
`, 'utf8');
  await chmod(fakeFfmpeg, 0o755);

  const previousArgsPath = process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
  process.env.FRAMEMIO_FFMPEG_ARGS_PATH = argsPath;
  try {
    const assets = fixtureMediaAssets();
    const assetPaths = new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`]));
    const composition = buildComposition(fixtureStoryPlan(), assets, assetPaths, new Map(), {
      work_dir: root,
      assets_dir: path.join(root, 'assets'),
      clips_dir: path.join(root, 'clips'),
      output_file: outputPath,
      composition_file: path.join(root, 'composition.json'),
    });

    await new FfmpegCompositionRenderer({ffmpegBinary: fakeFfmpeg}).renderComposition(composition, outputPath);

    const args = JSON.parse(await readFile(argsPath, 'utf8')) as string[];
    const inputs = args.flatMap((arg, index) => arg === '-i' ? [args[index + 1]] : []);
    assert.deepEqual(inputs, ['/asset/IMG_1001.jpg', '/asset/IMG_1002.MOV']);
    const filterComplex = args[args.indexOf('-filter_complex') + 1];
    assert.match(filterComplex, /\[0:v\]/);
    assert.match(filterComplex, /\[1:v\]/);
    assert.match(filterComplex, /zoompan=z=/);
    assert.match(filterComplex, /fade=t=out:st=2\.5500:d=0\.4500\[v0\]/);
    assert.match(filterComplex, /fade=t=in:st=0:d=0\.4500\[v1\]/);
    assert.match(filterComplex, /concat=n=2:v=1:a=0,format=yuv420p\[basev\]/);
    assert.match(filterComplex, /drawtext=textfile=.*:enable='between\(t,3\.200,4\.700\)'/);
    assert.match(filterComplex, /anullsrc=channel_layout=stereo:sample_rate=48000/);
    assert.match(filterComplex, /afade=t=out:st=2\.5500:d=0\.4500\[a0\]/);
    assert.match(filterComplex, /\[1:a\]atrim=duration=4\.500,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=0\.6480,afade=t=in:st=0:d=0\.4500\[a1\]/);
    assert.match(filterComplex, /\[a0\]\[a1\]concat=n=2:v=0:a=1\[originala\]/);
    assert.doesNotMatch(filterComplex, /\[bga\]/);
    assert.doesNotMatch(filterComplex, /amix=inputs=2/);
    assert.deepEqual(args.flatMap((arg, index) => arg === '-map' ? [args[index + 1]] : []), ['[outv]', '[outa]']);
    assert.equal(args.includes('-an'), false);
    assert.equal(args.includes('-c:a'), true);
  } finally {
    if (previousArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousArgsPath;
    }
  }
});


test('ffmpeg fallback renderer applies dynamic caption style', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-caption-style-render-test-'));
  const fakeFfmpeg = path.join(root, 'fake-ffmpeg.cjs');
  const argsPath = path.join(root, 'ffmpeg-args.json');
  const outputPath = path.join(root, 'story.mp4');
  await writeFile(fakeFfmpeg, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(process.env.FRAMEMIO_FFMPEG_ARGS_PATH, JSON.stringify(args));
fs.writeFileSync(args[args.length - 1], 'fake mp4');
`, 'utf8');
  await chmod(fakeFfmpeg, 0o755);

  const previousArgsPath = process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
  process.env.FRAMEMIO_FFMPEG_ARGS_PATH = argsPath;
  try {
    const assets = fixtureMediaAssets();
    const assetPaths = new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`]));
    const composition = buildComposition(fixtureStoryPlan({
      caption_style: {
        preset: 'clean_minimal',
        font_family: 'inter-bold',
        font_size: 58,
        text_color: '#FFE066',
        stroke_color: '#101010',
        stroke_width: 6,
        background_enabled: false,
        position: 'top',
        bottom_offset: 42,
        max_lines: 2,
        max_characters_per_line: 24,
      },
    }), assets, assetPaths, new Map(), {
      work_dir: root,
      assets_dir: path.join(root, 'assets'),
      clips_dir: path.join(root, 'clips'),
      output_file: outputPath,
      composition_file: path.join(root, 'composition.json'),
    });

    await new FfmpegCompositionRenderer({ffmpegBinary: fakeFfmpeg}).renderComposition(composition, outputPath);

    const args = JSON.parse(await readFile(argsPath, 'utf8')) as string[];
    const filterComplex = args[args.indexOf('-filter_complex') + 1];
    assert.match(filterComplex, /font='DejaVu Sans Bold'/);
    assert.match(filterComplex, /fontcolor=0xFFE066/);
    assert.match(filterComplex, /fontsize=58/);
    assert.match(filterComplex, /borderw=6/);
    assert.match(filterComplex, /bordercolor=0x101010@0\.72/);
    assert.match(filterComplex, /box=0/);
    assert.match(filterComplex, /y=42/);
  } finally {
    if (previousArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousArgsPath;
    }
  }
});

test('ffmpeg fallback renderer renders typography before captions', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-typography-render-test-'));
  const fakeFfmpeg = path.join(root, 'fake-ffmpeg.cjs');
  const argsPath = path.join(root, 'ffmpeg-args.json');
  const outputPath = path.join(root, 'story.mp4');
  await writeFile(fakeFfmpeg, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(process.env.FRAMEMIO_FFMPEG_ARGS_PATH, JSON.stringify(args));
fs.writeFileSync(args[args.length - 1], 'fake mp4');
`, 'utf8');
  await chmod(fakeFfmpeg, 0o755);

  const previousArgsPath = process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
  process.env.FRAMEMIO_FFMPEG_ARGS_PATH = argsPath;
  try {
    const assets = fixtureMediaAssets();
    const assetPaths = new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`]));
    const basePlan = fixtureStoryPlan();
    const composition = buildComposition(fixtureStoryPlan({
      moments: [{
        ...basePlan.moments[0],
        typography: {
          enabled: true,
          type: 'moment_title',
          title: 'Golden Hour',
          subtitle: 'Somewhere between the sea and the sky.',
          style: 'cinematic',
          placement: 'bottom_left',
          start_ms: 350,
          duration_ms: 2200,
          max_width: 0.72,
          safe_area: {top: 0.08, right: 0.08, bottom: 0.08, left: 0.08},
          animation: 'fade_up',
          source: 'ai',
          confidence: 0.91,
        },
      }, basePlan.moments[1]],
    }), assets, assetPaths, new Map(), {
      work_dir: root,
      assets_dir: path.join(root, 'assets'),
      clips_dir: path.join(root, 'clips'),
      output_file: outputPath,
      composition_file: path.join(root, 'composition.json'),
    });

    await new FfmpegCompositionRenderer({ffmpegBinary: fakeFfmpeg}).renderComposition(composition, outputPath);

    const args = JSON.parse(await readFile(argsPath, 'utf8')) as string[];
    const filterComplex = args[args.indexOf('-filter_complex') + 1];
    assert.match(filterComplex, /typography-0-title-/);
    assert.match(filterComplex, /typography-0-subtitle-/);
    assert.match(filterComplex, /font='Liberation Serif'/);
    assert.match(filterComplex, /fontsize=78/);
    assert.match(filterComplex, /x=w\*0\.0800/);
    assert.match(filterComplex, /enable='between\(t,0\.350,2\.550\)'/);
    assert.match(filterComplex, /alpha='if\(lt\(t\\,0\.350\+0\.350\)\\,/);
    assert.match(filterComplex, /y=h-.*\+if\(lt\(t\\,0\.350\+0\.350\)\\,/);
    assert.ok(filterComplex.indexOf('typography-0-title-') < filterComplex.indexOf('caption-0-'));
  } finally {
    if (previousArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousArgsPath;
    }
  }
});


test('ffmpeg fallback renderer strips audio when original audio is disabled', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-muted-ffmpeg-render-test-'));
  const fakeFfmpeg = path.join(root, 'fake-ffmpeg.cjs');
  const argsPath = path.join(root, 'ffmpeg-args.json');
  const outputPath = path.join(root, 'story.mp4');
  await writeFile(fakeFfmpeg, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(process.env.FRAMEMIO_FFMPEG_ARGS_PATH, JSON.stringify(args));
fs.writeFileSync(args[args.length - 1], 'fake mp4');
`, 'utf8');
  await chmod(fakeFfmpeg, 0o755);

  const previousArgsPath = process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
  process.env.FRAMEMIO_FFMPEG_ARGS_PATH = argsPath;
  try {
    const assets = fixtureMediaAssets();
    const assetPaths = new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`]));
    const composition = buildComposition(fixtureStoryPlan({audio: {source: 'none'}}), assets, assetPaths, new Map(), {
      work_dir: root,
      assets_dir: path.join(root, 'assets'),
      clips_dir: path.join(root, 'clips'),
      output_file: outputPath,
      composition_file: path.join(root, 'composition.json'),
    });

    await new FfmpegCompositionRenderer({ffmpegBinary: fakeFfmpeg}).renderComposition(composition, outputPath);

    const args = JSON.parse(await readFile(argsPath, 'utf8')) as string[];
    const filterComplex = args[args.indexOf('-filter_complex') + 1];
    assert.doesNotMatch(filterComplex, /\[outa\]/);
    assert.deepEqual(args.flatMap((arg, index) => arg === '-map' ? [args[index + 1]] : []), ['[outv]']);
    assert.equal(args.includes('-an'), true);
  } finally {
    if (previousArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousArgsPath;
    }
  }
});


test('ffmpeg fallback renderer converts HEIC photos before rendering', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-heic-render-test-'));
  const fakeFfmpeg = path.join(root, 'fake-ffmpeg.cjs');
  const fakeHeifConvert = path.join(root, 'fake-heif-convert.cjs');
  const heifArgsPath = path.join(root, 'heif-args.json');
  const ffmpegArgsPath = path.join(root, 'ffmpeg-args.json');
  const outputPath = path.join(root, 'story.mp4');
  await writeFile(fakeFfmpeg, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(process.env.FRAMEMIO_FFMPEG_ARGS_PATH, JSON.stringify(args));
fs.writeFileSync(args[args.length - 1], 'fake mp4');
`, 'utf8');
  await writeFile(fakeHeifConvert, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(process.env.FRAMEMIO_HEIF_ARGS_PATH, JSON.stringify(args));
fs.writeFileSync(args[1], 'fake jpeg');
`, 'utf8');
  await chmod(fakeFfmpeg, 0o755);
  await chmod(fakeHeifConvert, 0o755);

  const previousFfmpegArgsPath = process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
  const previousHeifArgsPath = process.env.FRAMEMIO_HEIF_ARGS_PATH;
  process.env.FRAMEMIO_FFMPEG_ARGS_PATH = ffmpegArgsPath;
  process.env.FRAMEMIO_HEIF_ARGS_PATH = heifArgsPath;
  try {
    const [photo] = fixtureMediaAssets();
    const heicPhoto = {...photo, file_name: 'IMG_0111.HEIC', content_type: 'image/heic'};
    const storyPlan = fixtureStoryPlan({moments: [{...fixtureStoryPlan().moments[0], asset_id: heicPhoto.id}]});
    const composition = buildComposition(storyPlan, [heicPhoto], new Map([[heicPhoto.id, '/asset/IMG_0111.HEIC']]), new Map(), {
      work_dir: root,
      assets_dir: path.join(root, 'assets'),
      clips_dir: path.join(root, 'clips'),
      output_file: outputPath,
      composition_file: path.join(root, 'composition.json'),
    });

    await new FfmpegCompositionRenderer({ffmpegBinary: fakeFfmpeg, heifConvertBinary: fakeHeifConvert}).renderComposition(composition, outputPath);

    assert.deepEqual(JSON.parse(await readFile(heifArgsPath, 'utf8')), ['/asset/IMG_0111.HEIC', path.join(root, `${composition.clips[0].clip_id}.jpg`)]);
    const ffmpegArgs = JSON.parse(await readFile(ffmpegArgsPath, 'utf8')) as string[];
    assert.equal(ffmpegArgs[ffmpegArgs.indexOf('-i') + 1], path.join(root, `${composition.clips[0].clip_id}.jpg`));
  } finally {
    if (previousFfmpegArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousFfmpegArgsPath;
    }
    if (previousHeifArgsPath === undefined) {
      delete process.env.FRAMEMIO_HEIF_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_HEIF_ARGS_PATH = previousHeifArgsPath;
    }
  }
});

test('ffmpeg fallback renderer skips HEIC conversion for JPEG bytes', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-mislabeled-heic-render-test-'));
  const fakeFfmpeg = path.join(root, 'fake-ffmpeg.cjs');
  const fakeHeifConvert = path.join(root, 'fake-heif-convert.cjs');
  const ffmpegArgsPath = path.join(root, 'ffmpeg-args.json');
  const outputPath = path.join(root, 'story.mp4');
  const mislabeledPhotoPath = path.join(root, 'IMG_0111.HEIC');
  await writeFile(mislabeledPhotoPath, Buffer.from([0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43]));
  await writeFile(fakeFfmpeg, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(process.env.FRAMEMIO_FFMPEG_ARGS_PATH, JSON.stringify(args));
fs.writeFileSync(args[args.length - 1], 'fake mp4');
`, 'utf8');
  await writeFile(fakeHeifConvert, `#!/usr/bin/env node
process.exit(42);
`, 'utf8');
  await chmod(fakeFfmpeg, 0o755);
  await chmod(fakeHeifConvert, 0o755);

  const previousFfmpegArgsPath = process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
  process.env.FRAMEMIO_FFMPEG_ARGS_PATH = ffmpegArgsPath;
  try {
    const [photo] = fixtureMediaAssets();
    const heicPhoto = {...photo, file_name: 'IMG_0111.HEIC', content_type: 'image/heic'};
    const storyPlan = fixtureStoryPlan({moments: [{...fixtureStoryPlan().moments[0], asset_id: heicPhoto.id}]});
    const composition = buildComposition(storyPlan, [heicPhoto], new Map([[heicPhoto.id, mislabeledPhotoPath]]), new Map(), {
      work_dir: root,
      assets_dir: path.join(root, 'assets'),
      clips_dir: path.join(root, 'clips'),
      output_file: outputPath,
      composition_file: path.join(root, 'composition.json'),
    });

    await new FfmpegCompositionRenderer({ffmpegBinary: fakeFfmpeg, heifConvertBinary: fakeHeifConvert}).renderComposition(composition, outputPath);

    const ffmpegArgs = JSON.parse(await readFile(ffmpegArgsPath, 'utf8')) as string[];
    assert.equal(ffmpegArgs[ffmpegArgs.indexOf('-i') + 1], mislabeledPhotoPath);
  } finally {
    if (previousFfmpegArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousFfmpegArgsPath;
    }
  }
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
