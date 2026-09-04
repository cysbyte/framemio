import {spawn} from 'node:child_process';

export interface JobMessage {
  jobId: string;
  projectId: string;
  mediaId?: string;
  jobType: string;
  configuration?: Record<string, string>;
}

export interface ReceivedJobMessage {
  message: JobMessage;
  receiptHandle: string;
  receiveCount: number;
}

interface AwsCliQueueOptions {
  queueURL: string;
  awsBinary?: string;
}

export class AwsCliQueue {
  private readonly awsBinary: string;

  constructor(private readonly options: AwsCliQueueOptions) {
    this.awsBinary = options.awsBinary ?? 'aws';
  }

  async receive(maxMessages: number, waitTimeSeconds: number, visibilityTimeoutSeconds: number): Promise<ReceivedJobMessage[]> {
    const output = await runJSON(this.awsBinary, [
      'sqs', 'receive-message',
      '--queue-url', this.options.queueURL,
      '--max-number-of-messages', String(Math.max(1, Math.min(10, maxMessages))),
      '--wait-time-seconds', String(Math.max(0, Math.min(20, waitTimeSeconds))),
      '--visibility-timeout', String(Math.max(0, visibilityTimeoutSeconds)),
      '--attribute-names', 'ApproximateReceiveCount',
      '--output', 'json',
    ]);
    const messages = Array.isArray(output.Messages) ? output.Messages : [];
    return messages.flatMap((raw: Record<string, unknown>) => {
      const body = typeof raw.Body === 'string' ? raw.Body : '';
      const receiptHandle = typeof raw.ReceiptHandle === 'string' ? raw.ReceiptHandle : '';
      if (!body || !receiptHandle) {
        return [];
      }
      try {
        const message = JSON.parse(body) as JobMessage;
        const attributes = raw.Attributes && typeof raw.Attributes === 'object' ? raw.Attributes as Record<string, string> : {};
        return [{message, receiptHandle, receiveCount: Number(attributes.ApproximateReceiveCount ?? 1)}];
      } catch {
        return [];
      }
    });
  }

  async delete(receiptHandle: string): Promise<void> {
    await run(this.awsBinary, ['sqs', 'delete-message', '--queue-url', this.options.queueURL, '--receipt-handle', receiptHandle]);
  }

  async release(receiptHandle: string): Promise<void> {
    await this.defer(receiptHandle, 0);
  }

  async defer(receiptHandle: string, visibilityTimeoutSeconds: number): Promise<void> {
    await run(this.awsBinary, ['sqs', 'change-message-visibility', '--queue-url', this.options.queueURL, '--receipt-handle', receiptHandle, '--visibility-timeout', String(Math.max(0, Math.floor(visibilityTimeoutSeconds)))]);
  }
}

async function runJSON(command: string, args: string[]): Promise<Record<string, unknown>> {
  const stdout = await run(command, args);
  if (stdout.trim() === '') {
    return {};
  }
  return JSON.parse(stdout) as Record<string, unknown>;
}

async function run(command: string, args: string[]): Promise<string> {
  return await new Promise<string>((resolve, reject) => {
    const child = spawn(command, args, {stdio: ['ignore', 'pipe', 'pipe']});
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => {
      stdout += String(chunk);
    });
    child.stderr.on('data', (chunk) => {
      stderr += String(chunk);
    });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        resolve(stdout);
        return;
      }
      reject(new Error(`${command} exited with ${code}: ${stderr}`));
    });
  });
}
