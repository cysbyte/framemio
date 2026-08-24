import type {RenderInput} from './types.js';

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

  private async request(path: string, init: RequestInit): Promise<Response> {
    if (!this.baseURL || !this.internalToken.trim()) {
      throw new Error('backend url and internal token are required');
    }
    const response = await fetch(this.baseURL + path, {
      ...init,
      headers: {
        ...(init.headers ?? {}),
        'X-FrameMio-Internal-Token': this.internalToken,
      },
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(`backend ${init.method ?? 'GET'} ${path} returned ${response.status}: ${text.slice(0, 500)}`);
    }
    return response;
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
