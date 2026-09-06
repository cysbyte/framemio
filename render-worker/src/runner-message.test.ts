import test from 'node:test';
import assert from 'node:assert/strict';
import {handleMessage, type RunnerConfig} from './runner.js';
import {BackendRequestError} from './backend-client.js';
import type {ReceivedJobMessage} from './aws-cli.js';
import type {RenderInput} from './types.js';

const baseConfig: RunnerConfig = {
  queueURL: 'queue',
  backendURL: 'https://api.framemio.com',
  internalToken: 'internal-token-with-at-least-thirty-two-characters',
  bucket: 'bucket',
  workRoot: '/tmp/framemio-render-test',
  runOnce: true,
  waitTimeoutMs: 1000,
  visibilityTimeoutSeconds: 900,
  maxReceiveCount: 5,
  rendererMode: 'ffmpeg',
  remotionEntryPoint: 'entry.tsx',
};

test('deletes terminal render queue messages before rendering', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-completed', receiveCount: 1});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: backendSpy({status: 'COMPLETED'}, calls),
    worker: workerSpy(calls),
    config: baseConfig,
  });

  assert.equal(handled, true);
  assert.deepEqual(calls, ['jobStatus:job-completed', 'delete:receipt-job-completed']);
});

test('deletes queue messages whose jobs no longer exist', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-missing', receiveCount: 1});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: backendSpy(null, calls),
    worker: workerSpy(calls),
    config: baseConfig,
  });

  assert.equal(handled, true);
  assert.deepEqual(calls, ['jobStatus:job-missing', 'delete:receipt-job-missing']);
});

test('defers active non-render messages', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-story-plan', jobType: 'STORY_PLAN', receiveCount: 5});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: backendSpy({status: 'QUEUED'}, calls),
    worker: workerSpy(calls),
    config: baseConfig,
  });

  assert.equal(handled, false);
  assert.deepEqual(calls, ['jobStatus:job-story-plan', 'defer:receipt-job-story-plan:1']);
});

test('defers active non-target render messages in run-once mode', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-other-render', receiveCount: 1});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: backendSpy({status: 'QUEUED'}, calls),
    worker: workerSpy(calls),
    config: {...baseConfig, jobID: 'job-target-render'},
  });

  assert.equal(handled, false);
  assert.deepEqual(calls, ['jobStatus:job-other-render', 'defer:receipt-job-other-render:1']);
});

test('deletes stale render messages when callbacks report missing job', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-deleted', receiveCount: 330});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: {
      async jobStatus(jobID: string): Promise<{status: string} | null> {
        calls.push(`jobStatus:${jobID}`);
        throw new BackendRequestError('status endpoint unavailable', 405, 'GET', `/internal/jobs/${jobID}/status`);
      },
      async markRunning(jobID: string): Promise<void> {
        calls.push(`markRunning:${jobID}`);
        throw new BackendRequestError('record not found', 404, 'POST', `/internal/jobs/${jobID}/status`);
      },
      async renderInput(jobID: string): Promise<RenderInput> {
        calls.push(`renderInput:${jobID}`);
        throw new Error('renderInput should not be called');
      },
      async markCompleted(jobID: string): Promise<void> {
        calls.push(`markCompleted:${jobID}`);
      },
      async markFailed(jobID: string): Promise<void> {
        calls.push(`markFailed:${jobID}`);
        throw new BackendRequestError('record not found', 404, 'POST', `/internal/jobs/${jobID}/status`);
      },
    },
    worker: workerSpy(calls),
    config: baseConfig,
  });

  assert.equal(handled, true);
  assert.deepEqual(calls, ['jobStatus:job-deleted', 'markRunning:job-deleted', 'markFailed:job-deleted', 'delete:receipt-job-deleted']);
});

function renderMessage(overrides: {jobId: string; jobType?: string; receiveCount: number}): ReceivedJobMessage {
  return {
    message: {
      jobId: overrides.jobId,
      projectId: 'project-1',
      jobType: overrides.jobType ?? 'VIDEO_RENDER',
    },
    receiptHandle: `receipt-${overrides.jobId}`,
    receiveCount: overrides.receiveCount,
  };
}

function queueSpy(calls: string[]) {
  return {
    async delete(receiptHandle: string): Promise<void> {
      calls.push(`delete:${receiptHandle}`);
    },
    async defer(receiptHandle: string, seconds: number): Promise<void> {
      calls.push(`defer:${receiptHandle}:${seconds}`);
    },
  };
}

function backendSpy(status: {status: string} | null, calls: string[]) {
  return {
    async jobStatus(jobID: string): Promise<{status: string} | null> {
      calls.push(`jobStatus:${jobID}`);
      return status;
    },
    async markRunning(jobID: string): Promise<void> {
      calls.push(`markRunning:${jobID}`);
    },
    async renderInput(jobID: string): Promise<RenderInput> {
      calls.push(`renderInput:${jobID}`);
      throw new Error('renderInput should not be called');
    },
    async markCompleted(jobID: string): Promise<void> {
      calls.push(`markCompleted:${jobID}`);
    },
    async markFailed(jobID: string): Promise<void> {
      calls.push(`markFailed:${jobID}`);
    },
  };
}

function workerSpy(calls: string[]) {
  return {
    async render(): Promise<{record: {s3_key: string}}> {
      calls.push('render');
      throw new Error('render should not be called');
    },
  };
}
