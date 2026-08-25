import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
import path from 'node:path';

const packageRoot = path.dirname(path.dirname(fileURLToPath(import.meta.url)));

async function readPackageManifest(): Promise<Record<string, any>> {
  return JSON.parse(await readFile(path.join(packageRoot, 'package.json'), 'utf8'));
}

// The Dockerfile runtime stage sets NODE_ENV=production before `npm ci`, so npm
// omits devDependencies. Anything the start script needs at runtime must
// therefore be a regular dependency. When tsx sat in devDependencies the image
// built and pushed cleanly, then every container died with
// `sh: 1: tsx: not found` (exit 127) — VIDEO_RENDER jobs stayed QUEUED forever
// and the app hung on "Creating your film...".
test('every binary the start script invokes is a production dependency', async () => {
  const manifest = await readPackageManifest();
  const startScript: string = manifest.scripts?.start ?? '';
  assert.ok(startScript.length > 0, 'start script must exist');

  const interpreter = startScript.trim().split(/\s+/)[0];
  const dependencies = manifest.dependencies ?? {};
  const devDependencies = manifest.devDependencies ?? {};

  assert.ok(
    Object.hasOwn(dependencies, interpreter),
    `start script runs "${interpreter}", so it must be in dependencies, not devDependencies`
  );
  assert.ok(
    !Object.hasOwn(devDependencies, interpreter),
    `"${interpreter}" must not also be declared in devDependencies`
  );
});

// npm ci refuses to run when the lockfile disagrees with package.json, which
// would break the image build rather than the container.
test('lockfile records the start interpreter as a production dependency', async () => {
  const manifest = await readPackageManifest();
  const interpreter = (manifest.scripts?.start ?? '').trim().split(/\s+/)[0];
  const lock = JSON.parse(await readFile(path.join(packageRoot, 'package-lock.json'), 'utf8'));

  const rootDependencies = lock.packages?.['']?.dependencies ?? {};
  assert.ok(
    Object.hasOwn(rootDependencies, interpreter),
    `lockfile root must list "${interpreter}" under dependencies; run npm install to refresh it`
  );

  const entry = lock.packages?.[`node_modules/${interpreter}`];
  assert.ok(entry, `lockfile must contain node_modules/${interpreter}`);
  assert.notEqual(
    entry.dev,
    true,
    `"${interpreter}" is marked dev-only in the lockfile, so NODE_ENV=production npm ci will skip it`
  );
});
