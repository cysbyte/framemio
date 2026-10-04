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
  shutdownReleaseVisibilitySeconds: 15,
  maxReceiveCount: 5,
  rendererMode: 'ffmpeg',
  remotionEntryPoint: 'entry.tsx',
  hyperframesEnabled: false,
  hyperframesTimeoutMs: 300000,
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

test('releases active non-render messages without delaying the queue', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-story-plan', jobType: 'STORY_PLAN', receiveCount: 5});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: backendSpy({status: 'QUEUED'}, calls),
    worker: workerSpy(calls),
    config: baseConfig,
  });

  assert.equal(handled, false);
  assert.deepEqual(calls, ['jobStatus:job-story-plan', 'defer:receipt-job-story-plan:0']);
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

test('defers render messages when status preflight fails', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-deleted', receiveCount: 330});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: {
      async jobStatus(jobID: string): Promise<{status: string} | null> {
        calls.push(`jobStatus:${jobID}`);
        throw new BackendRequestError('status endpoint unavailable', 405, 'GET', `/internal/jobs/${jobID}/status`);
      },
      async markRunning(jobID: string): Promise<void> { calls.push(`markRunning:${jobID}`); },
      async renderInput(jobID: string): Promise<RenderInput> {
        calls.push(`renderInput:${jobID}`);
        throw new Error('renderInput should not be called');
      },
      async markCompleted(jobID: string): Promise<void> {
        calls.push(`markCompleted:${jobID}`);
      },
      async markFailed(jobID: string): Promise<void> { calls.push(`markFailed:${jobID}`); },
    },
    worker: workerSpy(calls),
    config: baseConfig,
  });

  assert.equal(handled, false);
  assert.deepEqual(calls, ['jobStatus:job-deleted', 'defer:receipt-job-deleted:30']);
});

test('retains rendered message when completion callback is transiently unavailable', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-completion-retry', receiveCount: 2});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: {
      async jobStatus(jobID: string): Promise<{status: string} | null> { calls.push(`jobStatus:${jobID}`); return {status: 'QUEUED'}; },
      async markRunning(jobID: string): Promise<void> { calls.push(`markRunning:${jobID}`); },
      async renderInput(jobID: string): Promise<RenderInput> {
        calls.push(`renderInput:${jobID}`);
        return {
          job_id: jobID,
          project_id: 'project-1',
          output_s3_key: 'renders/completed.mp4',
          story_plan: {id: 'story-plan-1', moments: [], captions: []},
          media_assets: [],
        } as unknown as RenderInput;
      },
      async markCompleted(jobID: string): Promise<void> { calls.push(`markCompleted:${jobID}`); throw new BackendRequestError('temporarily unavailable', 503, 'POST', `/internal/jobs/${jobID}/status`); },
      async markFailed(jobID: string): Promise<void> { calls.push(`markFailed:${jobID}`); },
    },
    worker: {
      async render(): Promise<{record: {s3_key: string}}> { calls.push('render'); return {record: {s3_key: 'renders/completed.mp4'}}; },
    },
    taskProtection: taskProtectionSpy(calls),
    config: baseConfig,
  });

  assert.equal(handled, false);
  assert.deepEqual(calls, [
    'jobStatus:job-completion-retry',
    'protect',
    'markRunning:job-completion-retry',
    'renderInput:job-completion-retry',
    'render',
    'markCompleted:job-completion-retry',
    'defer:receipt-job-completion-retry:30',
    'unprotect',
  ]);
});

test('protects the ECS task before rendering and releases protection after success', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-protected-render', receiveCount: 1});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: successfulBackendSpy(calls),
    worker: {
      async render(): Promise<{record: {s3_key: string}}> {
        calls.push('render');
        return {record: {s3_key: 'renders/protected.mp4'}};
      },
    },
    taskProtection: taskProtectionSpy(calls),
    config: baseConfig,
  });

  assert.equal(handled, true);
  assert.deepEqual(calls, [
    'jobStatus:job-protected-render',
    'protect',
    'markRunning:job-protected-render',
    'renderInput:job-protected-render',
    'render',
    'markCompleted:job-protected-render:renders/protected.mp4',
    'delete:receipt-job-protected-render',
    'unprotect',
  ]);
});

test('defers the render message when ECS task protection cannot be enabled', async () => {
  const calls: string[] = [];
  const received = renderMessage({jobId: 'job-protection-failed', receiveCount: 1});
  const handled = await handleMessage(received, {
    queue: queueSpy(calls),
    backend: successfulBackendSpy(calls),
    worker: workerSpy(calls),
    taskProtection: {
      async protect(): Promise<boolean> {
        calls.push('protect');
        throw new Error('task protection unavailable');
      },
      async unprotect(): Promise<void> {
        calls.push('unprotect');
      },
    },
    config: baseConfig,
  });

  assert.equal(handled, false);
  assert.deepEqual(calls, [
    'jobStatus:job-protection-failed',
    'protect',
    'defer:receipt-job-protection-failed:30',
  ]);
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

function successfulBackendSpy(calls: string[]) {
  return {
    async jobStatus(jobID: string): Promise<{status: string}> {
      calls.push(`jobStatus:${jobID}`);
      return {status: 'QUEUED'};
    },
    async markRunning(jobID: string): Promise<void> {
      calls.push(`markRunning:${jobID}`);
    },
    async renderInput(jobID: string): Promise<RenderInput> {
      calls.push(`renderInput:${jobID}`);
      return {
        job_id: jobID,
        project_id: 'project-1',
        output_s3_key: 'renders/protected.mp4',
        story_plan: {id: 'story-plan-1', moments: [], captions: []},
        media_assets: [],
      } as unknown as RenderInput;
    },
    async markCompleted(jobID: string, artifactS3Key: string): Promise<void> {
      calls.push(`markCompleted:${jobID}:${artifactS3Key}`);
    },
    async markFailed(jobID: string): Promise<void> {
      calls.push(`markFailed:${jobID}`);
    },
  };
}

function taskProtectionSpy(calls: string[]) {
  return {
    async protect(): Promise<boolean> {
      calls.push('protect');
      return true;
    },
    async unprotect(): Promise<void> {
      calls.push('unprotect');
    },
  };
}
