import {spawn} from 'node:child_process';
import {mkdir, writeFile} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import type {CompositionSpec} from '../types.js';
import {canonicalJson, stableHash} from '../determinism.js';

export interface RemotionRenderer {
  renderComposition(composition: CompositionSpec, outputPath: string): Promise<void>;
}

export class FixtureRemotionRenderer implements RemotionRenderer {
  readonly rendered: CompositionSpec[] = [];

  async renderComposition(composition: CompositionSpec, outputPath: string): Promise<void> {
    this.rendered.push(composition);
    await mkdir(path.dirname(outputPath), {recursive: true});
    await writeFile(outputPath, canonicalJson(composition), 'utf8');
  }
}

export interface RemotionCliRendererOptions {
  entryPoint: string;
  compositionID?: string;
  remotionBinary?: string;
  timeoutMs?: number;
}

export class RemotionCliRenderer implements RemotionRenderer {
  private readonly compositionID: string;
  private readonly remotionBinary: string;
  private readonly timeoutMs: number;

  constructor(private readonly options: RemotionCliRendererOptions) {
    this.compositionID = options.compositionID ?? 'FramemioStory';
    this.remotionBinary = options.remotionBinary ?? 'npx';
    this.timeoutMs = options.timeoutMs ?? 15 * 60 * 1000;
  }

  async renderComposition(composition: CompositionSpec, outputPath: string): Promise<void> {
    await mkdir(path.dirname(outputPath), {recursive: true});
    const propsPath = path.join(tmpdir(), `framemio-remotion-${stableHash(composition).slice(0, 20)}.json`);
    await writeFile(propsPath, canonicalJson({composition}), 'utf8');
    const args = this.remotionBinary === 'npx'
      ? ['remotion', 'render', this.options.entryPoint, this.compositionID, outputPath]
      : ['render', this.options.entryPoint, this.compositionID, outputPath];
    args.push('--props', propsPath, '--codec', 'h264', '--overwrite', '--concurrency', '1');
    await run(this.remotionBinary, args, this.timeoutMs);
  }
}

async function run(command: string, args: string[], timeoutMs: number): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, args, {stdio: ['ignore', 'pipe', 'pipe']});
    const timeout = setTimeout(() => {
      child.kill('SIGTERM');
      reject(new Error(`${command} timed out after ${timeoutMs}ms`));
    }, timeoutMs);
    let stderr = '';
    child.stderr.on('data', (chunk) => {
      stderr += String(chunk);
    });
    child.on('error', (error) => {
      clearTimeout(timeout);
      reject(error);
    });
    child.on('close', (code) => {
      clearTimeout(timeout);
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} exited with ${code}: ${stderr}`));
    });
  });
}
