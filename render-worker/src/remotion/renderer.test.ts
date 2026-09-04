import test from 'node:test';
import assert from 'node:assert/strict';
import {chmod, mkdtemp, readFile, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {RemotionCliRenderer} from './renderer.js';
import type {CompositionSpec} from '../types.js';

test('remotion cli renderer exposes local media through public dir', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'framemio-remotion-cli-test-'));
  const sourcePath = path.join(root, 'photo.jpg');
  const outputPath = path.join(root, 'out.mp4');
  const argsPath = path.join(root, 'args.json');
  const fakeRemotion = path.join(root, 'fake-remotion.mjs');
  await writeFile(sourcePath, 'fake image bytes');
  await writeFile(fakeRemotion, `#!/usr/bin/env node\nimport {writeFileSync} from 'node:fs';\nwriteFileSync(${JSON.stringify(argsPath)}, JSON.stringify(process.argv.slice(2)));\nwriteFileSync(process.argv[5], 'fake video');\n`);
  await chmod(fakeRemotion, 0o755);

  const composition: CompositionSpec = {
    renderer_version: 'framemio-renderer-v1',
    story_plan_id: 'plan-1',
    story_plan_version: 1,
    project_id: 'project-1',
    fps: 30,
    resolution: {width: 1080, height: 1920},
    duration_frames: 30,
    duration_ms: 1000,
    clips: [{
      clip_id: 'clip-1', asset_id: 'photo-1', media_type: 'photo', prepared_path: sourcePath,
      source_path: sourcePath, start_frame: 0, duration_frames: 30, duration_ms: 1000,
      crop_intent: 'fit', transition: 'cut', speed: 1, volume: 0,
      transform: {kind: 'photo', from_scale: 1, to_scale: 1.02, from_x: 0, from_y: 0, to_x: 0, to_y: 0},
    }],
    broll: [],
    captions: [],
    audio: {source: 'none', original_audio: false, original_volume: 0},
  };

  await new RemotionCliRenderer({entryPoint: 'entry.tsx', remotionBinary: fakeRemotion, timeoutMs: 5000}).renderComposition(composition, outputPath);

  const args = JSON.parse(await readFile(argsPath, 'utf8')) as string[];
  const propsPath = args[args.indexOf('--props') + 1];
  const publicDir = args[args.indexOf('--public-dir') + 1];
  const props = JSON.parse(await readFile(propsPath, 'utf8')) as {composition: CompositionSpec};
  assert.equal(args[0], 'render');
  assert.ok(publicDir.includes('framemio-remotion-public-'));
  assert.match(props.composition.clips[0].prepared_path, /^remotion-static:asset-0-/);
});
