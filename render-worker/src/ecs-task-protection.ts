const defaultProtectionMinutes = 120;
const defaultRequestTimeoutMs = 5000;

export interface TaskProtectionClient {
  protect(): Promise<boolean>;
  unprotect(): Promise<void>;
}

export class EcsTaskProtectionClient implements TaskProtectionClient {
  constructor(
    private readonly agentURI = process.env.ECS_AGENT_URI?.trim() ?? '',
    private readonly protectionMinutes = defaultProtectionMinutes,
    private readonly requestTimeoutMs = defaultRequestTimeoutMs,
  ) {}

  async protect(): Promise<boolean> {
    if (!this.agentURI) {
      return false;
    }
    await this.update({ProtectionEnabled: true, ExpiresInMinutes: this.protectionMinutes});
    return true;
  }

  async unprotect(): Promise<void> {
    if (!this.agentURI) {
      return;
    }
    await this.update({ProtectionEnabled: false});
  }

  private async update(body: {ProtectionEnabled: boolean; ExpiresInMinutes?: number}): Promise<void> {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), this.requestTimeoutMs);
    try {
      const response = await fetch(`${this.agentURI.replace(/\/+$/, '')}/task-protection/v1/state`, {
        method: 'PUT',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(body),
        signal: controller.signal,
      });
      if (!response.ok) {
        const text = await response.text();
        throw new Error(`ECS task protection returned ${response.status}: ${text.slice(0, 500)}`);
      }
    } finally {
      clearTimeout(timeout);
    }
  }
}
