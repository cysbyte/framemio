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
import {brollTrace, DeterministicRenderWorker} from './render-worker.js';
import {buildComposition} from './timeline.js';
import {fixtureMediaAssets, fixtureRenderInput, fixtureStoryPlan} from '../__fixtures__/story-plan.js';

async function createHarness(compositionOutputIsFinal = false) {
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
    compositionOutputIsFinal,
    now: () => new Date('2026-08-21T12:00:00.000Z'),
  });
  return {root, objectStore, ffmpeg, remotion, worker};
}

test('b-roll trace preserves IDs and distinguishes missing scores', () => {
  assert.equal(
    brollTrace([
      {broll_id: 'broll-a', asset_id: 'media-a', start_ms: 1000, duration_ms: 1200, score: 0.72, confidence: 0.81},
      {asset_id: 'media-b', start_ms: 2000, duration_ms: 900, confidence: 0.64},
    ]),
    'broll-a:media-a:start=1000:duration=1200:score=0.720:confidence=0.810,broll_index_1:media-b:start=2000:duration=900:score=unset:confidence=0.640',
  );
});

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

test('normalizes a photo-over-video overlay without replacing the base video', () => {
  const assets = fixtureMediaAssets();
  const basePlan = fixtureStoryPlan({
    template_id: 'photo-over-video',
    template_version: 1,
    template_configuration: {
      aspect_ratio: '9:16',
      duration_seconds: 8,
      visual_style: 'cinematic',
      captions_enabled: true,
      caption_style: 'cinematic_subtitle',
      scene_detail: 'balanced',
      layout: 'picture_in_picture',
      overlay_placement: 'bottom_right',
      overlay_size: 'medium',
      overlay_opacity: 0.8,
      overlay_frame: 'subtle',
      overlay_animation: 'fade',
    },
    broll: [{
      broll_id: 'photo-overlay',
      asset_id: 'photo-2',
      media_type: 'photo',
      start_ms: 1200,
      duration_ms: 1800,
      presentation: 'picture_in_picture',
      placement: 'bottom_right',
      size: 'medium',
      opacity: 0.8,
      frame: 'subtle',
      animation: 'fade',
      reason: 'supporting_photo',
    }],
  });
  const composition = buildComposition(
    basePlan,
    assets,
    new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`])),
    new Map(),
    {
      work_dir: '/work',
      assets_dir: '/work/assets',
      clips_dir: '/work/clips',
      output_file: '/work/story.mp4',
      composition_file: '/work/composition.json',
    },
  );

  assert.equal(composition.clips.some((clip) => clip.media_type === 'video'), true);
  assert.equal(composition.broll.length, 1);
  assert.equal(composition.broll[0].presentation, 'picture_in_picture');
  assert.equal(composition.broll[0].start_frame, 36);
  assert.equal(composition.broll[0].duration_frames, 54);
  assert.equal(composition.broll[0].placement, 'bottom_right');
  assert.equal(composition.broll[0].size, 'medium');
  assert.equal(composition.broll[0].opacity, 0.8);
});

test('accepts concrete scale and move b-roll animations', () => {
  const assets = fixtureMediaAssets();
  for (const animation of ['scale', 'move'] as const) {
    const composition = buildComposition(
      fixtureStoryPlan({
        template_id: 'photo-over-video',
        template_version: 1,
        template_configuration: {
          aspect_ratio: '9:16',
          duration_seconds: 8,
          layout: 'picture_in_picture',
          overlay_placement: 'bottom_right',
          overlay_size: 'medium',
          overlay_opacity: 1,
          overlay_frame: 'subtle',
          overlay_animation: animation,
        },
        broll: [{
          asset_id: 'photo-2',
          media_type: 'photo',
          start_ms: 1000,
          duration_ms: 1500,
          presentation: 'picture_in_picture',
          placement: 'bottom_right',
          size: 'medium',
          opacity: 1,
          frame: 'subtle',
          animation,
        }],
      }),
      assets,
      new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`])),
      new Map(),
      {
        work_dir: '/work',
        assets_dir: '/work/assets',
        clips_dir: '/work/clips',
        output_file: '/work/story.mp4',
        composition_file: '/work/composition.json',
      },
    );
    assert.equal(composition.broll[0].animation, animation);
  }
});

test('normalizes montage b-roll to full-frame cutaways', () => {
  const assets = fixtureMediaAssets();
  const composition = buildComposition(
    fixtureStoryPlan({
      template_id: 'fast-montage',
      template_version: 1,
      template_configuration: {
        aspect_ratio: '9:16',
        duration_seconds: 15,
        layout: 'montage',
        overlay_placement: 'center',
        overlay_size: 'large',
        overlay_opacity: 1,
        overlay_frame: 'none',
        overlay_animation: 'none',
      },
      broll: [{
        asset_id: 'photo-2',
        media_type: 'photo',
        start_ms: 1000,
        duration_ms: 1500,
        presentation: 'montage',
        placement: 'center',
        size: 'large',
        opacity: 1,
        frame: 'none',
        animation: 'none',
      }],
    }),
    assets,
    new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`])),
    new Map(),
    {
      work_dir: '/work',
      assets_dir: '/work/assets',
      clips_dir: '/work/clips',
      output_file: '/work/story.mp4',
      composition_file: '/work/composition.json',
    },
  );

  assert.equal(composition.broll.length, 1);
  assert.equal(composition.broll[0].presentation, 'full_frame');
});

test('normalizes a full-source composition longer than 60 seconds', () => {
  const assets = fixtureMediaAssets();
  const fullSourceMoment = {
    ...fixtureStoryPlan().moments[1],
    moment_id: 'moment-video-full-source',
    start_micros: 0,
    end_micros: 300_000_000,
    duration_ms: 300_000,
    speed: undefined,
  };
  const composition = buildComposition(
    fixtureStoryPlan({
      template_id: 'talking-head-reel',
      template_version: 1,
      template_configuration: {
        aspect_ratio: '9:16',
        duration_mode: 'full_source',
        remove_filler_and_pauses: true,
        visual_style: 'minimal',
        captions_enabled: true,
        caption_style: 'bold_social',
        scene_detail: 'more',
        layout: 'picture_in_picture',
      },
      moments: [fullSourceMoment],
      captions: [{moment_id: 'moment-video-full-source', text: 'Full source caption', start_ms: 120_000, end_ms: 124_000}],
      output: {aspect_ratio: '9:16', max_duration_seconds: 600, preset: 'social'},
      requested_duration_ms: 300_000,
      total_duration_ms: 300_000,
    }),
    assets,
    new Map(assets.map((asset) => [asset.id, `/asset/${asset.file_name}`])),
    new Map(),
    {
      work_dir: '/work',
      assets_dir: '/work/assets',
      clips_dir: '/work/clips',
      output_file: '/work/story.mp4',
      composition_file: '/work/composition.json',
    },
  );

  assert.equal(composition.duration_ms, 300_000);
  assert.equal(composition.duration_frames, 9000);
  assert.equal(composition.template_configuration?.duration_mode, 'full_source');
  assert.equal(composition.template_configuration?.remove_filler_and_pauses, true);
  assert.equal(composition.captions[0].start_ms, 120_000);
});

test('rejects unsupported structured b-roll layout and unknown media', async () => {
  const invalidLayout = fixtureRenderInput({
    story_plan: fixtureStoryPlan({
      broll: [{asset_id: 'photo-2', start_ms: 1000, duration_ms: 1000, presentation: 'freeform' as never}],
    }),
  });
  const harness = await createHarness();
  await assert.rejects(() => harness.worker.render(invalidLayout), /unsupported b-roll presentation/);
  const invalidMedia = fixtureRenderInput({
    story_plan: fixtureStoryPlan({
      broll: [{asset_id: 'unknown', start_ms: 1000, duration_ms: 1000}],
    }),
  });
  await assert.rejects(() => harness.worker.render(invalidMedia), /invalid b-roll overlay/);
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

test('uploads the ffmpeg composition output without a redundant encode', async () => {
  const harness = await createHarness(true);
  const input = fixtureRenderInput();

  const result = await harness.worker.render(input);

  assert.equal(harness.ffmpeg.operations.some((operation) => operation.name === 'encodeMp4'), false);
  assert.equal(await harness.objectStore.objectExists(input.output_s3_key), true);
  assert.equal(
    await readFile(harness.objectStore.localPath(input.output_s3_key), 'utf8'),
    canonicalJson(result.composition),
  );
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

test('carries promo scene metadata and typography into timeline clips', () => {
  const basePlan = fixtureStoryPlan();
  const plan = fixtureStoryPlan({
    project_type: 'promo',
    call_to_action: 'Try it free',
    moments: [{
      ...basePlan.moments[0],
      order: 1,
      scene_purpose: 'hook',
      scene_text: {headline: 'Launch faster', supporting: 'AI-assisted promos', cta: 'Try it free'},
      voiceover: {text: 'Create a polished promo in minutes.', start_ms: 0, duration_ms: 1800},
      typography: {
        enabled: true,
        type: 'promo_headline',
        title: 'Launch faster',
        subtitle: 'AI-assisted promos',
        style: 'editorial',
        placement: 'bottom_left',
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

  assert.equal(composition.project_type, 'promo');
  assert.equal(composition.scenes?.[0].id, composition.clips[0].scene_id);
  assert.equal(composition.scenes?.[0].overlays?.[0]?.title, 'Launch faster');
  assert.equal(composition.text?.[0]?.title, 'Launch faster');
  assert.equal(composition.cta?.text, 'Try it free');
  assert.equal(composition.call_to_action, 'Try it free');
  assert.equal(composition.clips[0].order, 1);
  assert.equal(composition.clips[0].scene_purpose, 'hook');
  assert.deepEqual(composition.clips[0].scene_text, {headline: 'Launch faster', supporting: 'AI-assisted promos', cta: 'Try it free'});
  assert.deepEqual(composition.clips[0].voiceover, {text: 'Create a polished promo in minutes.', start_ms: 0, duration_ms: 1800});
  assert.equal(composition.clips[0].typography?.type, 'promo_headline');
  assert.equal(composition.clips[0].typography?.title, 'Launch faster');
});

test('builds normalized Story, Promo, and Reel compositions from shared fixtures', async () => {
  const cases = [
    {
      projectType: 'story' as const,
      plan: fixtureStoryPlan({project_type: 'story'}),
      assert: (composition: Awaited<ReturnType<DeterministicRenderWorker['render']>>['composition']) => {
        assert.equal(composition.canvas?.aspect_ratio, '9:16');
        assert.equal(composition.scenes?.length, composition.clips.length);
        assert.equal(composition.text?.length, 0);
      },
    },
    {
      projectType: 'promo' as const,
      plan: fixtureStoryPlan({
        project_type: 'promo',
        call_to_action: 'Try it today',
        moments: [{
          ...fixtureStoryPlan().moments[0],
          scene_text: {headline: 'Create faster', supporting: 'AI-assisted video editing', cta: 'Try it today'},
          typography: {enabled: true, title: 'Create faster', subtitle: 'AI-assisted video editing', style: 'editorial', placement: 'bottom_left'},
        }],
        captions: [],
        audio: {source: 'none'},
        output: {aspect_ratio: '16:9', max_duration_seconds: 15, preset: 'social'},
        total_duration_ms: 3000,
      }),
      assert: (composition: Awaited<ReturnType<DeterministicRenderWorker['render']>>['composition']) => {
        assert.deepEqual(composition.resolution, {width: 1920, height: 1080});
        assert.equal(composition.cta?.text, 'Try it today');
        assert.equal(composition.scenes?.[0].overlays?.[0]?.title, 'Create faster');
      },
    },
    {
      projectType: 'reel' as const,
      plan: fixtureStoryPlan({
        project_type: 'reel',
        moments: fixtureStoryPlan().moments.map((moment) => ({...moment, crop_intent: 'fill', typography: {enabled: true, title: 'Watch this', style: 'minimal', placement: 'top_center', safe_area: {top: 0.08, right: 0.08, bottom: 0.16, left: 0.08}}})),
        caption_style: {preset: 'bold_social', font_size: 52, bottom_offset: 160, position: 'bottom'},
        output: {aspect_ratio: '9:16', max_duration_seconds: 15, preset: 'social'},
      }),
      assert: (composition: Awaited<ReturnType<DeterministicRenderWorker['render']>>['composition']) => {
        assert.deepEqual(composition.resolution, {width: 1080, height: 1920});
        assert.equal(composition.scenes?.every((scene) => scene.crop_intent === 'fill'), true);
        assert.equal(composition.canvas?.safe_area?.bottom, 0.16);
      },
    },
  ];

  for (const testCase of cases) {
    const harness = await createHarness();
    const result = await harness.worker.render(fixtureRenderInput({
      project_type: testCase.projectType,
      story_plan: testCase.plan,
    }));
    assert.equal(result.composition.project_type, testCase.projectType);
    assert.ok(result.composition.canvas);
    assert.ok(result.composition.scenes);
    assert.ok(result.composition.media);
    assert.ok(result.composition.transitions);
    assert.ok(result.composition.effects);
    testCase.assert(result.composition);
  }
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
        presentation: 'word_by_word',
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
    assert.match(filterComplex, /font='Noto Sans Bold'/);
    assert.match(filterComplex, /fontcolor=0xFFE066/);
    assert.match(filterComplex, /fontsize=58/);
    assert.match(filterComplex, /borderw=6/);
    assert.match(filterComplex, /bordercolor=0x101010@0\.72/);
    assert.match(filterComplex, /box=0/);
    assert.match(filterComplex, /y=42/);
    assert.match(filterComplex, /caption-0-0-/);
    assert.match(filterComplex, /caption-0-1-/);
    assert.match(filterComplex, /enable='between\(t,3\.200,3\.950\)'/);
    assert.match(filterComplex, /enable='between\(t,3\.950,4\.700\)'/);
  } finally {
    if (previousArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousArgsPath;
    }
  }
});

test('ffmpeg fallback renderer paginates long full-text captions instead of truncating with ellipsis', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-caption-pagination-render-test-'));
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
      captions: [{
        moment_id: 'moment-video-1',
        text: 'Your camera roll is full of memories waiting to become a polished story.',
        start_ms: 3200,
        end_ms: 6200,
      }],
      caption_style: {
        preset: 'clean_minimal',
        font_family: 'inter-bold',
        font_size: 58,
        background_enabled: true,
        max_lines: 1,
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
    assert.match(filterComplex, /caption-0-0-/);
    assert.match(filterComplex, /caption-0-1-/);
    assert.match(filterComplex, /caption-0-2-/);
    assert.match(filterComplex, /enable='between\(t,3\.200,4\.200\)'/);
    assert.match(filterComplex, /enable='between\(t,4\.200,5\.200\)'/);
    assert.match(filterComplex, /enable='between\(t,5\.200,6\.200\)'/);

    const captionFileMatches = [...filterComplex.matchAll(/textfile='([^']*caption-0-\d-[^']+)'/g)];
    assert.equal(captionFileMatches.length, 3);
    const captionTexts = await Promise.all(captionFileMatches.map((match) => readFile(match[1], 'utf8')));
    assert.deepEqual(captionTexts, [
      'Your camera roll is full',
      'of memories waiting to',
      'become a polished story.',
    ]);
    assert.equal(captionTexts.some((text) => text.includes('...') || text.includes('…')), false);
  } finally {
    if (previousArgsPath === undefined) {
      delete process.env.FRAMEMIO_FFMPEG_ARGS_PATH;
    } else {
      process.env.FRAMEMIO_FFMPEG_ARGS_PATH = previousArgsPath;
    }
  }
});

test('ffmpeg fallback renderer includes typography overlays', async () => {
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
    assert.match(filterComplex, /typography-0-.*title-/);
    assert.match(filterComplex, /typography-0-.*subtitle-/);
    assert.match(filterComplex, /caption-0-/);
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

test('renders Story, Promo, and Reel through the same normalized composition', async () => {
  const cases = [
    {projectType: 'story' as const, aspectRatio: '9:16' as const},
    {projectType: 'promo' as const, aspectRatio: '16:9' as const},
    {projectType: 'reel' as const, aspectRatio: '9:16' as const},
  ];

  for (const testCase of cases) {
    const harness = await createHarness();
    const result = await harness.worker.render(fixtureRenderInput({
      project_type: testCase.projectType,
      project_configuration: {mode: testCase.projectType, brand: testCase.projectType === 'promo' ? {name: 'FrameMio'} : undefined},
      story_plan: fixtureStoryPlan({
        project_type: testCase.projectType,
        call_to_action: testCase.projectType === 'promo' ? 'Try it today' : undefined,
        output: {aspect_ratio: testCase.aspectRatio, max_duration_seconds: 15, preset: 'social'},
      }),
    }));
    assert.equal(result.composition.project_type, testCase.projectType);
    assert.equal(result.composition.project_configuration?.mode, testCase.projectType);
    assert.deepEqual(result.composition.resolution, resolutionForAspectRatio(testCase.aspectRatio));
    assert.equal(result.composition.clips.length, 2);
    if (testCase.projectType === 'promo') {
      assert.equal(result.composition.call_to_action, 'Try it today');
      assert.equal(result.composition.branding?.name, 'FrameMio');
      assert.deepEqual(result.composition.project_configuration?.brand, {name: 'FrameMio'});
    }
  }
});

test('rejects reel render input that is not vertical', async () => {
  const harness = await createHarness();
  await assert.rejects(
    () => harness.worker.render(fixtureRenderInput({
      project_type: 'reel',
      story_plan: fixtureStoryPlan({
        project_type: 'reel',
        output: {aspect_ratio: '1:1', max_duration_seconds: 15, preset: 'social'},
      }),
    })),
    RenderInputError,
  );
});

test('fails before rendering when source media is missing', async () => {
  const harness = await createHarness();
  const moment = fixtureStoryPlan().moments[0];
  const input = fixtureRenderInput({
    media_assets: [{...fixtureMediaAssets()[0], s3_key: 'source/missing.jpg'}],
    story_plan: fixtureStoryPlan({
      moments: [moment],
      requested_duration_ms: moment.duration_ms,
      total_duration_ms: moment.duration_ms,
    }),
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

test('rejects invalid composition inputs before rendering', async () => {
  const harness = await createHarness();
  await assert.rejects(
    () => harness.worker.render(fixtureRenderInput({story_plan: fixtureStoryPlan({total_duration_ms: 999_999})})),
    /duration mismatch/,
  );
  await assert.rejects(
    () => harness.worker.render(fixtureRenderInput({story_plan: fixtureStoryPlan({captions: [{moment_id: 'moment-photo-1', text: 'x'.repeat(221), start_ms: 0, end_ms: 1000}]})})),
    /captions\[0\]\.text exceeds 220/,
  );
  await assert.rejects(
    () => harness.worker.render(fixtureRenderInput({story_plan: fixtureStoryPlan({audio: {source: 'remote-music'}})})),
    /unsupported audio source/,
  );
  await assert.rejects(
    () => harness.worker.render(fixtureRenderInput({
      story_plan: fixtureStoryPlan({
        moments: [{...fixtureStoryPlan().moments[1], start_micros: 2_000_000, end_micros: 1_000_000, media_type: undefined}],
        captions: [],
        total_duration_ms: fixtureStoryPlan().moments[1].duration_ms,
      }),
    })),
    /invalid video source range/,
  );
});
