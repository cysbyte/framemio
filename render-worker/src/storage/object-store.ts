import {spawn} from 'node:child_process';
import {copyFile, mkdir, stat, writeFile} from 'node:fs/promises';
import path from 'node:path';

export interface ObjectStore {
  downloadObject(s3Key: string, destinationPath: string): Promise<void>;
  uploadObject(sourcePath: string, s3Key: string, contentType: string): Promise<void>;
  objectExists(s3Key: string): Promise<boolean>;
}

export class LocalFixtureObjectStore implements ObjectStore {
  constructor(private readonly rootDir: string) {}

  async downloadObject(s3Key: string, destinationPath: string): Promise<void> {
    await mkdir(path.dirname(destinationPath), {recursive: true});
    await copyFile(this.localPath(s3Key), destinationPath);
  }

  async uploadObject(sourcePath: string, s3Key: string, _contentType: string): Promise<void> {
    const destination = this.localPath(s3Key);
    await mkdir(path.dirname(destination), {recursive: true});
    await copyFile(sourcePath, destination);
  }

  async objectExists(s3Key: string): Promise<boolean> {
    try {
      await stat(this.localPath(s3Key));
      return true;
    } catch {
      return false;
    }
  }

  async putText(s3Key: string, value: string): Promise<void> {
    const destination = this.localPath(s3Key);
    await mkdir(path.dirname(destination), {recursive: true});
    await writeFile(destination, value, 'utf8');
  }

  localPath(s3Key: string): string {
    if (path.isAbsolute(s3Key) || s3Key.split('/').includes('..')) {
      throw new Error(`unsafe s3 key: ${s3Key}`);
    }
    return path.join(this.rootDir, s3Key);
  }
}

export interface AwsCliObjectStoreOptions {
  bucket: string;
  awsBinary?: string;
}

export class AwsCliObjectStore implements ObjectStore {
  private readonly awsBinary: string;

  constructor(private readonly options: AwsCliObjectStoreOptions) {
    this.awsBinary = options.awsBinary ?? 'aws';
  }

  async downloadObject(s3Key: string, destinationPath: string): Promise<void> {
    await mkdir(path.dirname(destinationPath), {recursive: true});
    await run(this.awsBinary, ['s3', 'cp', this.s3URI(s3Key), destinationPath, '--only-show-errors']);
  }

  async uploadObject(sourcePath: string, s3Key: string, contentType: string): Promise<void> {
    await run(this.awsBinary, ['s3', 'cp', sourcePath, this.s3URI(s3Key), '--content-type', contentType, '--only-show-errors']);
  }

  async objectExists(s3Key: string): Promise<boolean> {
    try {
      await run(this.awsBinary, ['s3api', 'head-object', '--bucket', this.options.bucket, '--key', safeKey(s3Key)]);
      return true;
    } catch {
      return false;
    }
  }

  private s3URI(s3Key: string): string {
    return `s3://${this.options.bucket}/${safeKey(s3Key)}`;
  }
}

function safeKey(s3Key: string): string {
  if (!s3Key || path.isAbsolute(s3Key) || s3Key.split('/').includes('..')) {
    throw new Error(`unsafe s3 key: ${s3Key}`);
  }
  return s3Key;
}

async function run(command: string, args: string[]): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, args, {stdio: ['ignore', 'pipe', 'pipe']});
    let stderr = '';
    child.stderr.on('data', (chunk) => {
      stderr += String(chunk);
    });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} exited with ${code}: ${stderr}`));
    });
  });
}
