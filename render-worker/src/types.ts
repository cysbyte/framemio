export const RENDER_VERSION = 'framemio-renderer-v1';

export type MediaType = 'photo' | 'video' | 'live_photo';
export type AspectRatio = '9:16' | '1:1' | '16:9';
export type TransitionType = 'cut' | 'fade' | 'dissolve';
export type CropIntent = 'fill' | 'fit' | 'pan' | 'zoom' | string;

export interface StoryPlan {
  schema_version: string;
  id: string;
  project_id: string;
  version: number;
  requested_duration_ms?: number;
  title_text?: string;
  ending_text?: string;
  music_mood?: string;
  moments: StoryMoment[];
  captions?: StoryCaption[];
  caption_style?: CaptionStyle;
  audio?: StoryAudio;
  output: StoryOutput;
  total_duration_ms?: number;
  created_at?: string;
}

export interface StoryMoment {
  moment_id?: string;
  asset_id: string;
  media_id?: string;
  media_type?: MediaType;
  video_analysis_version?: string;
  start_micros?: number;
  end_micros?: number;
  narrative?: string;
  caption?: string;
  duration_ms: number;
  crop_intent?: CropIntent;
  transition?: TransitionType | string;
  reason?: string;
  score?: number;
  speed?: number;
  volume?: number;
}

export interface StoryCaption {
  moment_id: string;
  text: string;
  start_ms?: number;
  end_ms?: number;
}

export interface CaptionStyle {
  preset?: 'bold_social' | 'cinematic_subtitle' | 'clean_minimal' | 'playful_pop' | string;
  font_family?: 'inter' | 'inter-bold' | 'system' | 'system-bold' | 'noto-sans' | 'noto-sans-bold' | string;
  font_size?: number;
  text_color?: string;
  stroke_color?: string;
  stroke_width?: number;
  background_enabled?: boolean;
  background_color?: string;
  background_opacity?: number;
  position?: 'bottom' | 'middle' | 'top' | string;
  bottom_offset?: number;
  max_lines?: number;
  max_characters_per_line?: number;
}

export interface StoryAudio {
  source: 'adaptive' | 'none' | 'licensed' | 'custom' | 'original' | string;
  mood?: string;
  background_s3_key?: string;
  original_audio?: boolean;
  background_volume?: number;
  original_volume?: number;
  ducking?: boolean;
  fade_in_ms?: number;
  fade_out_ms?: number;
}

export interface StoryOutput {
  aspect_ratio: AspectRatio | string;
  max_duration_seconds: number;
  preset: string;
}

export interface SourceMediaAsset {
  id: string;
  media_type: MediaType;
  s3_key: string;
  content_type: string;
  duration_seconds?: number;
  width?: number;
  height?: number;
  file_name?: string;
  etag?: string;
}

export interface RenderInput {
  job_id: string;
  project_id: string;
  story_plan: StoryPlan;
  media_assets: SourceMediaAsset[];
  output_s3_key: string;
  renderer_version?: string;
}

export type RenderStatus = 'queued' | 'running' | 'succeeded' | 'failed';

export interface RenderRecord {
  job_id: string;
  project_id: string;
  story_plan_id: string;
  story_plan_version: number;
  render_version: string;
  status: RenderStatus;
  output_resolution: Resolution;
  duration_ms: number;
  s3_key: string;
  content_type: 'video/mp4';
  error_code?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
}

export interface Resolution {
  width: number;
  height: number;
}

export interface RenderPaths {
  work_dir: string;
  assets_dir: string;
  clips_dir: string;
  output_file: string;
  composition_file: string;
}

export interface TimelineClip {
  clip_id: string;
  asset_id: string;
  media_type: MediaType;
  source_path: string;
  prepared_path: string;
  start_frame: number;
  duration_frames: number;
  duration_ms: number;
  source_start_micros?: number;
  source_end_micros?: number;
  crop_intent: string;
  transform: KenBurnsTransform | VideoTransform;
  transition: TransitionType;
  volume: number;
  speed: number;
}

export interface KenBurnsTransform {
  kind: 'photo';
  from_scale: number;
  to_scale: number;
  from_x: number;
  from_y: number;
  to_x: number;
  to_y: number;
}

export interface VideoTransform {
  kind: 'video';
  crop: 'fill' | 'fit';
  scale: number;
}

export interface CompositionSpec {
  renderer_version: string;
  story_plan_id: string;
  story_plan_version: number;
  project_id: string;
  fps: number;
  resolution: Resolution;
  duration_frames: number;
  duration_ms: number;
  title_text?: string;
  ending_text?: string;
  clips: TimelineClip[];
  captions: StoryCaption[];
  caption_style?: CaptionStyle;
  audio: ResolvedAudio;
}

export interface ResolvedAudio {
  source: string;
  mood?: string;
  background_path?: string;
  original_audio: boolean;
  background_volume: number;
  original_volume: number;
  ducking: boolean;
  fade_in_frames: number;
  fade_out_frames: number;
}

export interface RenderResult {
  record: RenderRecord;
  composition: CompositionSpec;
}
