import type {RenderInput} from './types.js';

export interface JobStatusResponse {
  job_id: string;
  job_type: string;
  status: string;
  project_id: string;
  media_id?: string;
  attempt: number;
}

export class BackendRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly method: string,
    readonly path: string,
  ) {
    super(message);
    this.name = 'BackendRequestError';
  }
}

export function isBackendRequestError(error: unknown): error is BackendRequestError {
  return error instanceof BackendRequestError;
}

export function isRetryableBackendError(error: unknown): error is BackendRequestError {
  return isBackendRequestError(error) && (error.status === 0 || error.status === 408 || error.status === 429 || error.status >= 500);
}

interface StatusRequest {
  status: 'RUNNING' | 'COMPLETED' | 'FAILED';
  progress: number;
  error_code?: string;
  error_message?: string;
  artifact_s3_key?: string;
  artifact_content_type?: string;
}

export class BackendClient {
  private readonly baseURL: string;

  constructor(baseURL: string, private readonly internalToken: string) {
    this.baseURL = baseURL.trim().replace(/\/+$/, '');
  }

  async renderInput(jobID: string): Promise<RenderInput> {
    const response = await this.request(`/internal/jobs/${encodeURIComponent(jobID)}/render-input`, {method: 'GET'});
    return await response.json() as RenderInput;
  }

  async jobStatus(jobID: string): Promise<JobStatusResponse | null> {
    const response = await this.request(`/internal/jobs/${encodeURIComponent(jobID)}/status`, {method: 'GET'}, [404]);
    if (response.status === 404) {
      return null;
    }
    return await response.json() as JobStatusResponse;
  }

  async markRunning(jobID: string, progress = 0.05): Promise<void> {
    await this.updateStatus(jobID, {status: 'RUNNING', progress});
  }

  async markCompleted(jobID: string, artifactS3Key: string): Promise<void> {
    await this.updateStatus(jobID, {
      status: 'COMPLETED',
      progress: 1,
      artifact_s3_key: artifactS3Key,
      artifact_content_type: 'video/mp4',
    });
  }

  async markFailed(jobID: string, error: unknown): Promise<void> {
    await this.updateStatus(jobID, {
      status: 'FAILED',
      progress: 0,
      error_code: 'render_worker_failed',
      error_message: errorMessage(error).slice(0, 2000),
    });
  }

  private async updateStatus(jobID: string, body: StatusRequest): Promise<void> {
    await this.request(`/internal/jobs/${encodeURIComponent(jobID)}/status`, {
      method: 'POST',
      body: JSON.stringify(body),
      headers: {'Content-Type': 'application/json'},
    });
  }

  private async request(path: string, init: RequestInit, allowedStatuses: number[] = []): Promise<Response> {
    if (!this.baseURL || !this.internalToken.trim()) {
      throw new Error('backend url and internal token are required');
    }
    const method = init.method ?? 'GET';
    for (let attempt = 1; attempt <= 3; attempt++) {
      let response: Response;
      try {
        response = await fetch(this.baseURL + path, {
          ...init,
          headers: {
            ...(init.headers ?? {}),
            'X-FrameMio-Internal-Token': this.internalToken,
          },
        });
      } catch (error) {
        const requestError = new BackendRequestError(`backend ${method} ${path} failed: ${errorMessage(error)}`, 0, method, path);
        if (attempt === 3) {
          throw requestError;
        }
        console.warn(JSON.stringify({level: 'warn', msg: 'backend request retrying after transport failure', method, path, attempt, next_attempt: attempt + 1, error: requestError.message}));
        await sleep(250 * 2 ** (attempt - 1));
        continue;
      }
      if (response.ok || allowedStatuses.includes(response.status)) {
        return response;
      }
      const text = await response.text();
      const requestError = new BackendRequestError(`backend ${method} ${path} returned ${response.status}: ${text.slice(0, 500)}`, response.status, method, path);
      if (!isRetryableBackendError(requestError) || attempt === 3) {
        throw requestError;
      }
      console.warn(JSON.stringify({level: 'warn', msg: 'backend request retrying after transient response', method, path, status_code: response.status, attempt, next_attempt: attempt + 1}));
      await sleep(250 * 2 ** (attempt - 1));
    }
    throw new Error(`backend ${method} ${path} failed without a response`);
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

async function sleep(durationMs: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, durationMs));
}
