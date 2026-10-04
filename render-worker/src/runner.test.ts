import test from 'node:test';
import assert from 'node:assert/strict';
import path from 'node:path';
import {loadConfig} from './runner.js';

test('loads render worker production config from render queue env', () => {
  const config = loadConfig({
    SQS_RENDER_QUEUE_URL: 'https://sqs.us-east-1.amazonaws.com/123/framemio-render',
    SQS_PROCESSING_QUEUE_URL: 'https://sqs.us-east-1.amazonaws.com/123/framemio-processing',
    BACKEND_INTERNAL_API_URL: 'https://api.framemio.com',
    INTERNAL_API_TOKEN: 'internal-token-with-at-least-thirty-two-characters',
    S3_MEDIA_BUCKET: 'framemio-media',
    WORKER_RUN_ONCE: 'true',
    WORKER_RUN_ONCE_WAIT_TIMEOUT: '3m',
    SQS_VISIBILITY_TIMEOUT: '900s',
    MAX_JOB_ATTEMPTS: '7',
    JOB_ID: 'job-render',
  });

  assert.equal(config.queueURL, 'https://sqs.us-east-1.amazonaws.com/123/framemio-render');
  assert.equal(config.backendURL, 'https://api.framemio.com');
  assert.equal(config.bucket, 'framemio-media');
  assert.equal(config.runOnce, true);
  assert.equal(config.jobID, 'job-render');
  assert.equal(config.waitTimeoutMs, 180_000);
  assert.equal(config.visibilityTimeoutSeconds, 900);
  assert.equal(config.maxReceiveCount, 7);
  assert.equal(config.rendererMode, 'ffmpeg');
  assert.equal(config.remotionEntryPoint, path.join(process.cwd(), 'src/remotion/entry.tsx'));
  assert.equal(config.hyperframesBrowserPath, '/usr/bin/chromium');
});

test('falls back to shared processing queue env for render worker config', () => {
  const config = loadConfig({
    SQS_PROCESSING_QUEUE_URL: 'https://sqs.us-east-1.amazonaws.com/123/framemio-processing',
    BACKEND_INTERNAL_API_URL: 'https://api.framemio.com',
    INTERNAL_API_TOKEN: 'internal-token-with-at-least-thirty-two-characters',
    S3_MEDIA_BUCKET: 'framemio-media',
  });

  assert.equal(config.queueURL, 'https://sqs.us-east-1.amazonaws.com/123/framemio-processing');
});

test('loads remotion mode with default and explicit entry points', () => {
  const base = {
    SQS_PROCESSING_QUEUE_URL: 'queue',
    BACKEND_INTERNAL_API_URL: 'https://api.framemio.com',
    INTERNAL_API_TOKEN: 'internal-token-with-at-least-thirty-two-characters',
    S3_MEDIA_BUCKET: 'framemio-media',
    RENDERER_MODE: 'remotion',
  };
  const defaultConfig = loadConfig(base);
  assert.equal(defaultConfig.rendererMode, 'remotion');
  assert.equal(defaultConfig.remotionEntryPoint, path.join(process.cwd(), 'src/remotion/entry.tsx'));

  const explicitConfig = loadConfig({...base, REMOTION_ENTRY_POINT: '/opt/render/remotion-entry.tsx'});
  assert.equal(explicitConfig.remotionEntryPoint, '/opt/render/remotion-entry.tsx');
});

test('loads hyperframes binary and browser path config', () => {
  const config = loadConfig({
    SQS_PROCESSING_QUEUE_URL: 'queue',
    BACKEND_INTERNAL_API_URL: 'https://api.framemio.com',
    INTERNAL_API_TOKEN: 'internal-token-with-at-least-thirty-two-characters',
    S3_MEDIA_BUCKET: 'framemio-media',
    HYPERFRAMES_ENABLED: 'true',
    HYPERFRAMES_BINARY: '/app/node_modules/.bin/hyperframes',
    HYPERFRAMES_BROWSER_PATH: '/custom/chromium',
  });

  assert.equal(config.hyperframesEnabled, true);
  assert.equal(config.hyperframesBinary, '/app/node_modules/.bin/hyperframes');
  assert.equal(config.hyperframesBrowserPath, '/custom/chromium');
});

test('requires backend callback config', () => {
  assert.throws(() => loadConfig({SQS_PROCESSING_QUEUE_URL: 'queue', S3_MEDIA_BUCKET: 'bucket'}), /BACKEND_INTERNAL_API_URL is required/);
});
