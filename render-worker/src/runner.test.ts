import test from 'node:test';
import assert from 'node:assert/strict';
import {loadConfig} from './runner.js';

test('loads render worker production config from shared processing queue env', () => {
  const config = loadConfig({
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

  assert.equal(config.queueURL, 'https://sqs.us-east-1.amazonaws.com/123/framemio-processing');
  assert.equal(config.backendURL, 'https://api.framemio.com');
  assert.equal(config.bucket, 'framemio-media');
  assert.equal(config.runOnce, true);
  assert.equal(config.jobID, 'job-render');
  assert.equal(config.waitTimeoutMs, 180_000);
  assert.equal(config.visibilityTimeoutSeconds, 900);
  assert.equal(config.maxReceiveCount, 7);
  assert.equal(config.rendererMode, 'ffmpeg');
});

test('requires backend callback config', () => {
  assert.throws(() => loadConfig({SQS_PROCESSING_QUEUE_URL: 'queue', S3_MEDIA_BUCKET: 'bucket'}), /BACKEND_INTERNAL_API_URL is required/);
});
