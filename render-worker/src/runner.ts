import {setTimeout as sleep} from 'node:timers/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {AwsCliQueue, type ReceivedJobMessage} from './aws-cli.js';
import {BackendClient, isBackendRequestError, isRetryableBackendError} from './backend-client.js';
import {EcsTaskProtectionClient, type TaskProtectionClient} from './ecs-task-protection.js';
import {ShellFfmpegClient} from './ffmpeg/client.js';
import {HyperframesCliRenderer} from './hyperframes/renderer.js';
import {FfmpegCompositionRenderer, RemotionCliRenderer} from './remotion/renderer.js';
import {AwsCliObjectStore} from './storage/object-store.js';
import {brollTrace, DeterministicRenderWorker} from './pipeline/render-worker.js';
import type {RenderInput} from './types.js';

const unsupportedMessageVisibilitySeconds = 0;
const nonTargetMessageVisibilitySeconds = 1;
const backendRetryMessageVisibilitySeconds = 30;
const defaultShutdownReleaseVisibilitySeconds = 15;

export interface RunnerConfig {
  queueURL: string;
  backendURL: string;
  internalToken: string;
  bucket: string;
  workRoot: string;
  jobID?: string;
  runOnce: boolean;
  waitTimeoutMs: number;
  visibilityTimeoutSeconds: number;
  shutdownReleaseVisibilitySeconds: number;
  maxReceiveCount: number;
  rendererMode: 'ffmpeg' | 'remotion';
  remotionEntryPoint: string;
  hyperframesEnabled: boolean;
  hyperframesBinary?: string;
  hyperframesBrowserPath?: string;
  hyperframesTimeoutMs: number;
}

export async function runRenderWorker(config = loadConfig()): Promise<void> {
  const queue = new AwsCliQueue({queueURL: config.queueURL});
  const backend = new BackendClient(config.backendURL, config.internalToken);
  const worker = new DeterministicRenderWorker({
    objectStore: new AwsCliObjectStore({bucket: config.bucket}),
    ffmpeg: new ShellFfmpegClient(),
    remotion: config.rendererMode === 'remotion'
      ? new RemotionCliRenderer({entryPoint: config.remotionEntryPoint})
      : new FfmpegCompositionRenderer(),
    hyperframes: config.hyperframesEnabled && config.hyperframesBinary
      ? new HyperframesCliRenderer({binary: config.hyperframesBinary, timeoutMs: config.hyperframesTimeoutMs, browserPath: config.hyperframesBrowserPath})
      : undefined,
    workRoot: config.workRoot,
    compositionOutputIsFinal: config.rendererMode === 'ffmpeg',
  });
  const taskProtection = new EcsTaskProtectionClient();
  let idleDeadline = Date.now() + config.waitTimeoutMs;
  let activeMessage: ReceivedJobMessage | undefined;
  const releasedReceiptHandles = new Set<string>();
  const shutdown = installShutdownHandler((signal) => {
    const message = activeMessage;
    if (!message || releasedReceiptHandles.has(message.receiptHandle)) {
      return;
    }
    releasedReceiptHandles.add(message.receiptHandle);
    const releaseStartedAt = Date.now();
    console.warn(JSON.stringify({level: 'warn', msg: 'render worker shutdown releasing active message', signal, job_id: message.message.jobId, project_id: message.message.projectId, receive_count: message.receiveCount, release_visibility_seconds: config.shutdownReleaseVisibilitySeconds}));
    void queue.defer(message.receiptHandle, config.shutdownReleaseVisibilitySeconds)
      .then(() => {
        console.warn(JSON.stringify({level: 'warn', msg: 'render worker shutdown active message released', signal, job_id: message.message.jobId, project_id: message.message.projectId, duration_ms: Date.now() - releaseStartedAt}));
      })
      .catch((error) => {
        console.error(JSON.stringify({level: 'error', msg: 'render worker shutdown active message release failed', signal, job_id: message.message.jobId, project_id: message.message.projectId, error: error instanceof Error ? error.message : String(error)}));
      });
  });
  console.info(JSON.stringify({level: 'info', msg: 'render worker started', run_once: config.runOnce, job_id: config.jobID ?? '', renderer_mode: config.rendererMode, hyperframes_enabled: config.hyperframesEnabled && Boolean(config.hyperframesBinary), queue_name: queueName(config.queueURL), visibility_timeout_seconds: config.visibilityTimeoutSeconds, shutdown_release_visibility_seconds: config.shutdownReleaseVisibilitySeconds}));

  try {
    do {
      const received = await queue.receive(10, 20, config.visibilityTimeoutSeconds);
      if (received.length > 0) {
        console.info(JSON.stringify({level: 'info', msg: 'render worker run-once received messages', run_once: config.runOnce, target_job_id: config.jobID ?? '', count: received.length}));
      }
      let handledMessage = false;
      for (const message of received) {
        activeMessage = message;
        let handledCurrentMessage = false;
        try {
          handledCurrentMessage = await handleMessage(message, {queue, backend, worker, taskProtection, config});
        } finally {
          if (activeMessage?.receiptHandle === message.receiptHandle) {
            activeMessage = undefined;
          }
        }
        if (handledCurrentMessage) {
          handledMessage = true;
          if (config.runOnce && (!config.jobID || message.message.jobId === config.jobID)) {
            console.info(JSON.stringify({level: 'info', msg: 'render worker run-once target handled', target_job_id: config.jobID ?? '', job_id: message.message.jobId}));
            return;
          }
          if (config.runOnce && config.jobID) {
            console.info(JSON.stringify({level: 'info', msg: 'render worker run-once continuing after available job', target_job_id: config.jobID, handled_job_id: message.message.jobId}));
          }
        }
      }
      if (config.runOnce && config.jobID && handledMessage) {
        idleDeadline = Date.now() + config.waitTimeoutMs;
      }
      if (config.runOnce && Date.now() >= idleDeadline) {
        console.error(JSON.stringify({level: 'error', msg: 'render worker run-once timed out before target', target_job_id: config.jobID ?? '', wait_timeout_ms: config.waitTimeoutMs}));
        throw new Error(`timed out waiting for render job ${config.jobID ?? ''}`);
      }
      if (shutdown.requested) {
        console.info(JSON.stringify({level: 'info', msg: 'render worker shutdown requested after current message'}));
        return;
      }
      if (!config.runOnce) {
        await sleep(1000);
      }
    } while ((!config.runOnce || Date.now() < idleDeadline) && !shutdown.requested);
  } finally {
    shutdown.dispose();
  }
}

function installShutdownHandler(onShutdown?: (signal: NodeJS.Signals) => void): {requested: boolean; dispose: () => void} {
  const state = {requested: false};
  const onSignal = (signal: NodeJS.Signals) => {
    state.requested = true;
    console.info(JSON.stringify({level: 'info', msg: 'render worker shutdown requested', signal}));
    onShutdown?.(signal);
  };
  process.once('SIGTERM', onSignal);
  process.once('SIGINT', onSignal);
  return {
    get requested() {
      return state.requested;
    },
    dispose: () => {
      process.off('SIGTERM', onSignal);
      process.off('SIGINT', onSignal);
    },
  };
}

interface RenderQueueClient {
  delete(receiptHandle: string): Promise<void>;
  defer(receiptHandle: string, visibilityTimeoutSeconds: number): Promise<void>;
}

interface RenderBackendClient {
  jobStatus(jobID: string): Promise<{status: string} | null>;
  markRunning(jobID: string, progress?: number): Promise<void>;
  renderInput(jobID: string): Promise<RenderInput>;
  markCompleted(jobID: string, artifactS3Key: string): Promise<void>;
  markFailed(jobID: string, error: unknown): Promise<void>;
}

interface RenderWorkerClient {
  render(input: RenderInput): Promise<{record: {s3_key: string}}>;
}

export async function handleMessage(received: ReceivedJobMessage, dependencies: {
  queue: RenderQueueClient;
  backend: RenderBackendClient;
  worker: RenderWorkerClient;
  taskProtection?: TaskProtectionClient;
  config: RunnerConfig;
}): Promise<boolean> {
  const {queue, backend, worker, taskProtection, config} = dependencies;
  const message = received.message;
  try {
    const status = await backend.jobStatus(message.jobId);
    if (!status) {
      console.warn(JSON.stringify({level: 'warn', msg: 'render worker deleting stale queue message', target_job_id: config.jobID ?? '', job_id: message.jobId, project_id: message.projectId, job_type: message.jobType, receive_count: received.receiveCount, reason: 'job_not_found'}));
      await queue.delete(received.receiptHandle);
      return true;
    }
    if (isTerminalJobStatus(status.status)) {
      console.info(JSON.stringify({level: 'info', msg: 'render worker deleting terminal queue message', target_job_id: config.jobID ?? '', job_id: message.jobId, project_id: message.projectId, job_type: message.jobType, status: status.status, receive_count: received.receiveCount}));
      await queue.delete(received.receiptHandle);
      return true;
    }
  } catch (error) {
    console.warn(JSON.stringify({level: 'warn', msg: 'render worker job status preflight failed; deferring message', target_job_id: config.jobID ?? '', job_id: message.jobId, project_id: message.projectId, job_type: message.jobType, receive_count: received.receiveCount, retry_visibility_seconds: backendRetryMessageVisibilitySeconds, retryable: isRetryableBackendError(error), error: error instanceof Error ? error.message : String(error)}));
    await queue.defer(received.receiptHandle, backendRetryMessageVisibilitySeconds);
    return false;
  }
  if (config.runOnce && config.jobID && message.jobId !== config.jobID) {
    console.info(JSON.stringify({level: 'info', msg: 'render worker deferring non-target message', target_job_id: config.jobID, job_id: message.jobId, project_id: message.projectId, job_type: message.jobType, receive_count: received.receiveCount, defer_seconds: nonTargetMessageVisibilitySeconds}));
    await queue.defer(received.receiptHandle, nonTargetMessageVisibilitySeconds);
    return false;
  }
  if (message.jobType !== 'VIDEO_RENDER') {
    console.warn(JSON.stringify({level: 'warn', msg: 'render worker ignoring unsupported queue message', target_job_id: config.jobID ?? '', job_id: message.jobId, project_id: message.projectId, job_type: message.jobType, receive_count: received.receiveCount, release_seconds: unsupportedMessageVisibilitySeconds}));
    await queue.defer(received.receiptHandle, unsupportedMessageVisibilitySeconds);
    return false;
  }
  let taskProtected = false;
  if (taskProtection) {
    try {
      taskProtected = await taskProtection.protect();
      if (taskProtected) {
        console.info(JSON.stringify({level: 'info', msg: 'render task scale-in protection enabled', job_id: message.jobId, project_id: message.projectId, receive_count: received.receiveCount}));
      }
    } catch (error) {
      console.error(JSON.stringify({level: 'error', msg: 'render task scale-in protection failed; deferring message', job_id: message.jobId, project_id: message.projectId, receive_count: received.receiveCount, retry_visibility_seconds: backendRetryMessageVisibilitySeconds, error: error instanceof Error ? error.message : String(error)}));
      await queue.defer(received.receiptHandle, backendRetryMessageVisibilitySeconds);
      return false;
    }
  }
  const startedAt = Date.now();
  let completionCallbackPending = false;
  console.info(JSON.stringify({level: 'info', msg: 'handling render message', job_id: message.jobId, project_id: message.projectId, receive_count: received.receiveCount}));
  try {
    const markRunningStartedAt = Date.now();
    await backend.markRunning(message.jobId, 0.08);
    console.info(JSON.stringify({level: 'info', msg: 'render mark running completed', job_id: message.jobId, duration_ms: Date.now() - markRunningStartedAt, elapsed_ms: Date.now() - startedAt}));
    const inputStartedAt = Date.now();
    const input = await backend.renderInput(message.jobId);
    console.info(JSON.stringify({level: 'info', msg: 'render input received', job_id: message.jobId, project_id: input.project_id, story_plan_id: input.story_plan.id, moment_count: input.story_plan.moments.length, broll_count: input.story_plan.broll?.length ?? 0, broll_trace: brollTrace(input.story_plan.broll ?? []), caption_count: input.story_plan.captions?.length ?? 0, media_asset_count: input.media_assets.length, duration_ms: Date.now() - inputStartedAt, elapsed_ms: Date.now() - startedAt}));
    const renderStartedAt = Date.now();
    const result = await worker.render(input);
    console.info(JSON.stringify({level: 'info', msg: 'render pipeline completed', job_id: message.jobId, output_s3_key: result.record.s3_key, duration_ms: Date.now() - renderStartedAt, elapsed_ms: Date.now() - startedAt}));
    const markCompletedStartedAt = Date.now();
    completionCallbackPending = true;
    await backend.markCompleted(message.jobId, result.record.s3_key);
    completionCallbackPending = false;
    console.info(JSON.stringify({level: 'info', msg: 'render mark completed callback completed', job_id: message.jobId, duration_ms: Date.now() - markCompletedStartedAt, elapsed_ms: Date.now() - startedAt}));
    await queue.delete(received.receiptHandle);
    console.info(JSON.stringify({level: 'info', msg: 'render message handled', job_id: message.jobId, output_s3_key: result.record.s3_key, total_duration_ms: Date.now() - startedAt}));
    return true;
  } catch (error) {
    console.error(JSON.stringify({level: 'error', msg: 'render message failed', job_id: message.jobId, duration_ms: Date.now() - startedAt, error: error instanceof Error ? error.message : String(error)}));
    if (completionCallbackPending || isRetryableBackendError(error)) {
      console.warn(JSON.stringify({level: 'warn', msg: 'render worker retaining message for backend retry', job_id: message.jobId, project_id: message.projectId, receive_count: received.receiveCount, retry_visibility_seconds: backendRetryMessageVisibilitySeconds, completion_callback_pending: completionCallbackPending, error: error instanceof Error ? error.message : String(error)}));
      await queue.defer(received.receiptHandle, backendRetryMessageVisibilitySeconds);
      return false;
    }
    try {
      await backend.markFailed(message.jobId, error);
    } catch (markFailedError) {
      if (isRetryableBackendError(markFailedError)) {
        console.warn(JSON.stringify({level: 'warn', msg: 'render worker retaining failed message for callback retry', job_id: message.jobId, project_id: message.projectId, receive_count: received.receiveCount, retry_visibility_seconds: backendRetryMessageVisibilitySeconds, error: markFailedError instanceof Error ? markFailedError.message : String(markFailedError)}));
        await queue.defer(received.receiptHandle, backendRetryMessageVisibilitySeconds);
        return false;
      }
      if (!isIgnorableTerminalCallbackError(markFailedError)) {
        throw markFailedError;
      }
      console.warn(JSON.stringify({level: 'warn', msg: 'render worker deleting stale failed message after callback rejection', job_id: message.jobId, project_id: message.projectId, receive_count: received.receiveCount, error: markFailedError instanceof Error ? markFailedError.message : String(markFailedError)}));
    }
    await queue.delete(received.receiptHandle);
    return true;
  } finally {
    if (taskProtected && taskProtection) {
      try {
        await taskProtection.unprotect();
        console.info(JSON.stringify({level: 'info', msg: 'render task scale-in protection disabled', job_id: message.jobId, project_id: message.projectId}));
      } catch (error) {
        console.error(JSON.stringify({level: 'error', msg: 'render task scale-in protection release failed', job_id: message.jobId, project_id: message.projectId, error: error instanceof Error ? error.message : String(error)}));
      }
    }
  }
}

function isTerminalJobStatus(status: string): boolean {
  return status === 'COMPLETED' || status === 'FAILED' || status === 'CANCELLED';
}

function isIgnorableTerminalCallbackError(error: unknown): boolean {
  return isBackendRequestError(error) && (error.status === 404 || error.status === 409);
}

export function loadConfig(environment = process.env): RunnerConfig {
  const rendererMode = (environment.RENDERER_MODE ?? environment.RENDER_MODE ?? 'ffmpeg').trim().toLowerCase();
  if (rendererMode !== 'ffmpeg' && rendererMode !== 'remotion') {
    throw new Error('RENDERER_MODE must be ffmpeg or remotion');
  }
  return {
    queueURL: required(first(environment.SQS_RENDER_QUEUE_URL, environment.SQS_PROCESSING_QUEUE_URL), 'SQS_RENDER_QUEUE_URL or SQS_PROCESSING_QUEUE_URL'),
    backendURL: required(first(environment.BACKEND_INTERNAL_API_URL, environment.BACKEND_URL, environment.API_BASE_URL), 'BACKEND_INTERNAL_API_URL'),
    internalToken: required(environment.INTERNAL_API_TOKEN, 'INTERNAL_API_TOKEN'),
    bucket: required(environment.S3_MEDIA_BUCKET, 'S3_MEDIA_BUCKET'),
    workRoot: environment.RENDER_WORK_ROOT ?? path.join(tmpdir(), 'framemio-render-worker'),
    jobID: environment.JOB_ID,
    runOnce: bool(environment.WORKER_RUN_ONCE, false),
    waitTimeoutMs: durationMs(environment.WORKER_RUN_ONCE_WAIT_TIMEOUT, 2 * 60 * 1000),
    visibilityTimeoutSeconds: Math.round(durationMs(environment.SQS_VISIBILITY_TIMEOUT, 15 * 60 * 1000) / 1000),
    shutdownReleaseVisibilitySeconds: Math.round(durationMs(environment.SHUTDOWN_RELEASE_VISIBILITY_TIMEOUT, defaultShutdownReleaseVisibilitySeconds * 1000) / 1000),
    maxReceiveCount: number(environment.MAX_JOB_ATTEMPTS, 5),
    rendererMode,
    remotionEntryPoint: environment.REMOTION_ENTRY_POINT ?? path.join(process.cwd(), 'src/remotion/entry.tsx'),
    hyperframesEnabled: bool(environment.HYPERFRAMES_ENABLED, false),
    hyperframesBinary: first(environment.HYPERFRAMES_BINARY, environment.HYPERFRAMES_CLI),
    hyperframesBrowserPath: environment.HYPERFRAMES_BROWSER_PATH ?? '/usr/bin/chromium',
    hyperframesTimeoutMs: durationMs(environment.HYPERFRAMES_TIMEOUT, 5 * 60 * 1000),
  };
}

function first(...values: Array<string | undefined>): string | undefined {
  return values.find((value) => value && value.trim() !== '');
}

function required(value: string | undefined, name: string): string {
  if (!value || value.trim() === '') {
    throw new Error(`${name} is required`);
  }
  return value.trim();
}

function bool(value: string | undefined, fallback: boolean): boolean {
  if (value === undefined || value === '') {
    return fallback;
  }
  return ['1', 'true', 'yes', 'enabled'].includes(value.trim().toLowerCase());
}

function number(value: string | undefined, fallback: number): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function durationMs(value: string | undefined, fallback: number): number {
  if (!value || value.trim() === '') {
    return fallback;
  }
  const match = value.trim().match(/^(\d+)(ms|s|m)?$/);
  if (!match) {
    return fallback;
  }
  const amount = Number(match[1]);
  const unit = match[2] ?? 'ms';
  if (unit === 'm') {
    return amount * 60_000;
  }
  if (unit === 's') {
    return amount * 1000;
  }
  return amount;
}

function queueName(queueURL: string): string {
  const trimmed = queueURL.trim().replace(/\/+$/, '');
  return trimmed.slice(trimmed.lastIndexOf('/') + 1);
}
