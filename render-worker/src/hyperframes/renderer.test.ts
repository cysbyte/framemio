import test from 'node:test';
import assert from 'node:assert/strict';
import {chmod, mkdtemp, readFile, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {applyHyperframesOverlays, HyperframesCliRenderer, hyperframesOverlayRequests, renderHyperframesOverlays} from './renderer.js';
import type {CompositionSpec} from '../types.js';

function composition(overrides: Partial<CompositionSpec> = {}): CompositionSpec {
  return {
    renderer_version: 'framemio-renderer-v1',
    story_plan_id: 'plan-1',
    story_plan_version: 1,
    project_id: 'project-1',
    fps: 30,
    resolution: {width: 1080, height: 1920},
    duration_frames: 150,
    duration_ms: 5000,
    clips: [{
      clip_id: 'clip-1',
      scene_id: 'moment-1',
      asset_id: 'video-1',
      media_type: 'video',
      source_path: '/source/video.mp4',
      prepared_path: '/prepared/video.mp4',
      start_frame: 0,
      duration_frames: 150,
      duration_ms: 5000,
      crop_intent: 'fill',
      transform: {kind: 'video', crop: 'fill', scale: 1},
      transition: 'cut',
      volume: 1,
      speed: 1,
    }],
    broll: [],
    captions: [{moment_id: 'moment-1', text: 'The biggest lesson', start_ms: 1000, end_ms: 2400}],
    audio: {source: 'none', original_audio: false, original_volume: 0},
    ...overrides,
  };
}

test('builds hyperframes requests only for whitelisted caption motion presets', () => {
  assert.deepEqual(hyperframesOverlayRequests(composition({caption_style: {motion_preset: 'raw_css_animation'}})), []);

  const requests = hyperframesOverlayRequests(composition({
    caption_style: {preset: 'bold_social', presentation: 'word_by_word', motion_preset: 'word_pop'},
  }));

  assert.equal(requests.length, 1);
  assert.equal(requests[0].kind, 'caption_overlay');
  assert.equal(requests[0].preset, 'word_pop');
  assert.equal(requests[0].start_ms, 1000);
  assert.equal(requests[0].duration_ms, 1400);
});

test('splits long hyperframes captions into short phrase overlays', () => {
  const requests = hyperframesOverlayRequests(composition({
    caption_style: {preset: 'bold_social', presentation: 'word_by_word', motion_preset: 'word_pop'},
    captions: [{moment_id: 'moment-1', text: 'Remember when the strawberries spilled on the picnic blanket', start_ms: 1000, end_ms: 3400}],
  }));

  assert.equal(requests.length, 3);
  assert.deepEqual(requests.map((request) => request.caption?.text), [
    'Remember when the strawberries',
    'spilled on the picnic',
    'blanket',
  ]);
  assert.deepEqual(requests.map((request) => request.duration_ms), [800, 800, 800]);
  assert.equal(requests.every((request) => request.source_caption?.text === 'Remember when the strawberries spilled on the picnic blanket'), true);
});

test('keeps hyperframes caption styling through the end of typical stories', () => {
  const captions = Array.from({length: 8}, (_, index) => ({
    moment_id: 'moment-1',
    text: `Caption ${index + 1} keeps this ending styled`,
    start_ms: index * 600,
    end_ms: index * 600 + 550,
  }));
  const source = composition({
    caption_style: {preset: 'bold_social', presentation: 'word_by_word', motion_preset: 'word_pop'},
    captions,
  });
  const requests = hyperframesOverlayRequests(source);

  assert.equal(requests.length, 16);
  const withOverlays = applyHyperframesOverlays(source, requests.map((request) => ({request, outputPath: `/tmp/${request.id}.webm`})));
  assert.equal(withOverlays.captions.length, 0);
  assert.equal(withOverlays.hyperframes?.overlays.length, 16);
});

test('renders hyperframes overlays through generated project and webm output', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-hyperframes-cli-test-'));
  const fakeHyperframes = path.join(root, 'fake-hyperframes.cjs');
  const argsPath = path.join(root, 'args.json');
  await writeFile(fakeHyperframes, `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.writeFileSync(${JSON.stringify(argsPath)}, JSON.stringify({args, browserPath: process.env.HYPERFRAMES_BROWSER_PATH}));
const output = args[args.indexOf('-o') + 1];
fs.writeFileSync(output, 'fake webm');
`, 'utf8');
  await chmod(fakeHyperframes, 0o755);

  const results = await renderHyperframesOverlays(
    composition({caption_style: {preset: 'bold_social', presentation: 'word_by_word', motion_preset: 'word_pop', highlight_color: '#FF3B30'}}),
    new HyperframesCliRenderer({binary: fakeHyperframes, timeoutMs: 5000, browserPath: '/usr/bin/chromium'}),
    root,
  );

  assert.equal(results.length, 1);
  assert.match(results[0].outputPath, /\.webm$/);
  const cliRun = JSON.parse(await readFile(argsPath, 'utf8')) as {args: string[]; browserPath?: string};
  const {args} = cliRun;
  assert.equal(cliRun.browserPath, '/usr/bin/chromium');
  assert.equal(args[0], 'render');
  assert.match(args[1], /hf-caption-0-.*-project$/);
  assert.equal(args[2], '-c');
  assert.equal(args[3], 'index.html');
  assert.equal(args[4], '--format');
  assert.equal(args[5], 'webm');
  assert.equal(args[6], '-o');
  assert.equal(args[7], results[0].outputPath);
  const html = await readFile(path.join(args[1], 'index.html'), 'utf8');
  assert.match(html, /@keyframes wordPop/);
  assert.match(html, /data-composition-id="main"/);
  assert.match(html, /data-start="0"/);
  assert.match(html, /data-duration="1\.400"/);
  assert.match(html, /data-width="1080"/);
  assert.match(html, /data-height="1920"/);
  assert.match(html, /data-fps="30"/);
  assert.match(html, /data-no-timeline="true"/);
  assert.match(html, /class="caption clip"/);
  assert.match(html, /data-track-index="0"/);
  assert.match(html, /background: transparent/);
  assert.match(html, /\.emphasis/);
  assert.match(html, /#FF3B30/);
  assert.match(html, /font-weight: 850/);
  assert.match(html, /-webkit-text-stroke: 0 transparent/);
  assert.match(html, /2px 0 0 #000000/);
  assert.match(html, /2px 2px 0 #000000/);
  assert.match(html, /animation: wordPop 1400ms linear both/);
  assert.doesNotMatch(html, /gsap\.timeline/);
  assert.match(html, /The <span class="emphasis">biggest<\/span> lesson/);
});

test('renders hyperframes overlays with bounded concurrency', async () => {
  let activeCount = 0;
  let peakActiveCount = 0;
  const rendered: string[] = [];
  const results = await renderHyperframesOverlays(
    composition({
      caption_style: {preset: 'bold_social', presentation: 'word_by_word', motion_preset: 'word_pop'},
      captions: [
        {moment_id: 'moment-1', text: 'First caption phrase', start_ms: 0, end_ms: 1000},
        {moment_id: 'moment-1', text: 'Second caption phrase', start_ms: 1000, end_ms: 2000},
        {moment_id: 'moment-1', text: 'Third caption phrase', start_ms: 2000, end_ms: 3000},
      ],
    }),
    {
      async renderOverlay(request, outputPath) {
        activeCount += 1;
        peakActiveCount = Math.max(peakActiveCount, activeCount);
        await new Promise((resolve) => setTimeout(resolve, 20));
        rendered.push(request.id);
        await writeFile(outputPath, 'fake webm', 'utf8');
        activeCount -= 1;
      },
    },
    await mkdtemp(path.join(tmpdir(), 'framemio-hyperframes-concurrency-test-')),
    {concurrency: 2},
  );

  assert.equal(peakActiveCount, 2);
  assert.equal(rendered.length, 3);
  assert.deepEqual(results.map((result) => result.request.id), ['hf-caption-0', 'hf-caption-1', 'hf-caption-2']);
});
