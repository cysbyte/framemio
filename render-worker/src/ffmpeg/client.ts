import {spawn} from 'node:child_process';
import {copyFile, mkdir, writeFile} from 'node:fs/promises';
import path from 'node:path';

export interface TrimVideoRequest {
  inputPath: string;
  outputPath: string;
  startMicros: number;
  endMicros: number;
  speed: number;
  keepAudio: boolean;
}

export interface EncodeRequest {
  compositionPath: string;
  intermediateVideoPath: string;
  outputPath: string;
  width: number;
  height: number;
  durationMs: number;
}

export interface FfmpegClient {
  trimVideo(request: TrimVideoRequest): Promise<void>;
  encodeMp4(request: EncodeRequest): Promise<void>;
}

export class ShellFfmpegClient implements FfmpegClient {
  constructor(private readonly ffmpegBinary = 'ffmpeg') {}

  async trimVideo(request: TrimVideoRequest): Promise<void> {
    await mkdir(path.dirname(request.outputPath), {recursive: true});
    const durationSeconds = Math.max(0.001, (request.endMicros - request.startMicros) / 1_000_000);
    const args = [
      '-hide_banner',
      '-loglevel',
      'error',
      '-y',
      '-ss',
      secondsArg(request.startMicros / 1_000_000),
      '-i',
      request.inputPath,
      '-t',
      secondsArg(durationSeconds),
      '-map_metadata',
      '-1',
      '-fflags',
      '+bitexact',
      '-flags:v',
      '+bitexact',
    ];

    const normalizedSpeed = clamp(request.speed, 0.25, 4);
    if (normalizedSpeed !== 1) {
      args.push('-filter:v', `setpts=${secondsArg(1 / normalizedSpeed)}*PTS`);
    }

    args.push('-c:v', 'libx264', '-preset', 'medium', '-crf', '18', '-pix_fmt', 'yuv420p');

    if (!request.keepAudio) {
      args.push('-an');
    } else {
      if (normalizedSpeed !== 1) {
        args.push('-filter:a', atempoFilter(normalizedSpeed));
      }
      args.push('-c:a', 'aac', '-b:a', '192k');
    }

    args.push(request.outputPath);
    await run(this.ffmpegBinary, args);
  }

  async encodeMp4(request: EncodeRequest): Promise<void> {
    await mkdir(path.dirname(request.outputPath), {recursive: true});
    await run(this.ffmpegBinary, [
      '-hide_banner',
      '-loglevel',
      'error',
      '-y',
      '-i',
      request.intermediateVideoPath,
      '-map_metadata',
      '-1',
      '-fflags',
      '+bitexact',
      '-flags:v',
      '+bitexact',
      '-c:v',
      'libx264',
      '-preset',
      'medium',
      '-crf',
      '18',
      '-pix_fmt',
      'yuv420p',
      '-movflags',
      '+faststart',
      request.outputPath,
    ]);
  }
}

export class DeterministicFixtureFfmpegClient implements FfmpegClient {
  readonly operations: Array<{name: 'trimVideo'; request: TrimVideoRequest} | {name: 'encodeMp4'; request: EncodeRequest}> = [];

  async trimVideo(request: TrimVideoRequest): Promise<void> {
    this.operations.push({name: 'trimVideo', request});
    await mkdir(path.dirname(request.outputPath), {recursive: true});
    await copyFile(request.inputPath, request.outputPath);
  }

  async encodeMp4(request: EncodeRequest): Promise<void> {
    this.operations.push({name: 'encodeMp4', request});
    await mkdir(path.dirname(request.outputPath), {recursive: true});
    await writeFile(request.outputPath, `fixture mp4\n${stableFixtureJson(request)}\n`, 'utf8');
  }
}

function atempoFilter(speed: number): string {
  const filters: number[] = [];
  let remaining = speed;
  while (remaining < 0.5) {
    filters.push(0.5);
    remaining /= 0.5;
  }
  while (remaining > 2) {
    filters.push(2);
    remaining /= 2;
  }
  filters.push(remaining);
  return filters.map((value) => `atempo=${secondsArg(value)}`).join(',');
}

function secondsArg(value: number): string {
  return value.toFixed(6);
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function stableFixtureJson(value: EncodeRequest): string {
  return JSON.stringify({
    compositionPath: value.compositionPath,
    durationMs: value.durationMs,
    height: value.height,
    intermediateVideoPath: value.intermediateVideoPath,
    outputPath: value.outputPath,
    width: value.width,
  });
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
