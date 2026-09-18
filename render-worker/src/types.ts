export const RENDER_VERSION = 'framemio-renderer-v1';

export type MediaType = 'photo' | 'video' | 'live_photo';
export type ProjectType = 'story' | 'promo' | 'reel';
export type AspectRatio = '9:16' | '1:1' | '16:9';
export type TransitionType = 'cut' | 'fade' | 'dissolve' | 'crossfade';
export type CropIntent = 'fill' | 'fit' | 'pan' | 'zoom' | string;

export interface StoryPlan {
  schema_version: string;
  id: string;
  project_id: string;
  project_type?: ProjectType;
  version: number;
  requested_duration_ms?: number;
  title_text?: string;
  ending_text?: string;
  call_to_action?: string;
  moments: StoryMoment[];
  broll?: StoryBRoll[];
  captions?: StoryCaption[];
  caption_style?: CaptionStyle;
  audio?: StoryAudio;
  output: StoryOutput;
  total_duration_ms?: number;
  created_at?: string;
}

export type EditPlan = StoryPlan;

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
  typography?: TextOverlay;
  order?: number;
  scene_purpose?: PromoScenePurpose;
  scene_text?: PromoSceneText;
  voiceover?: SceneVoiceover;
}

export type PromoScenePurpose = 'hook' | 'problem' | 'product' | 'feature' | 'benefit' | 'proof' | 'testimonial' | 'cta' | 'outro' | string;

export interface PromoSceneText {
  headline?: string;
  supporting?: string;
  cta?: string;
}

export interface SceneVoiceover {
  text?: string;
  start_ms?: number;
  duration_ms?: number;
}

export type TextOverlayType = 'moment_title' | 'moment_caption' | 'location' | 'date' | 'chapter_title' | 'story_intro' | 'story_outro' | string;
export type TypographyStyle = 'cinematic' | 'minimal' | 'editorial' | 'memory' | 'location' | string;
export type TextPlacement = 'top_left' | 'top_center' | 'top_right' | 'center_left' | 'center' | 'center_right' | 'bottom_left' | 'bottom_center' | 'bottom_right' | string;
export type TextAnimation = 'fade' | 'fade_up' | 'fade_down' | 'subtle_scale' | 'none' | string;
export type TextSource = 'ai' | 'user' | 'system' | string;

export interface SafeArea {
  top?: number;
  right?: number;
  bottom?: number;
  left?: number;
}

export interface TextOverlay {
  enabled: boolean;
  type?: TextOverlayType;
  title?: string;
  subtitle?: string;
  style?: TypographyStyle;
  placement?: TextPlacement;
  start_ms?: number;
  duration_ms?: number;
  max_width?: number;
  safe_area?: SafeArea;
  animation?: TextAnimation;
  source?: TextSource;
  reason?: string;
  confidence?: number;
}

export interface StoryBRoll {
  broll_id?: string;
  asset_id: string;
  media_id?: string;
  media_type?: MediaType;
  video_analysis_version?: string;
  source_start_micros?: number;
  source_end_micros?: number;
  start_ms: number;
  duration_ms: number;
  anchor?: StoryBRollAnchor;
  crop_intent?: CropIntent;
  reason?: string;
  confidence?: number;
  score?: number;
}

export interface StoryBRollAnchor {
  type: string;
  moment_id?: string;
  start_ms?: number;
  end_ms?: number;
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
  source?: 'none' | 'original' | string;
  original_audio?: boolean;
  original_volume?: number;
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
  project_type?: ProjectType;
  project_configuration?: Record<string, unknown>;
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
  scene_id?: string;
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
  overlays?: TextOverlay[];
  typography?: TextOverlay;
  order?: number;
  scene_purpose?: PromoScenePurpose;
  scene_text?: PromoSceneText;
  voiceover?: SceneVoiceover;
}

export interface TimelineBRoll {
  broll_id: string;
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
  project_type?: ProjectType;
  project_configuration?: Record<string, unknown>;
  story_plan_id: string;
  story_plan_version: number;
  project_id: string;
  canvas?: CanvasSpec;
  fps: number;
  resolution: Resolution;
  duration_frames: number;
  duration_ms: number;
  duration?: {frames: number; ms: number};
  title_text?: string;
  ending_text?: string;
  call_to_action?: string;
  cta?: CTASpec;
  branding?: BrandingSpec;
  media?: CompositionMediaAsset[];
  scenes?: TimelineScene[];
  text?: TextOverlay[];
  transitions?: CompositionTransition[];
  effects?: CompositionEffect[];
  clips: TimelineClip[];
  broll: TimelineBRoll[];
  captions: StoryCaption[];
  caption_style?: CaptionStyle;
  audio: ResolvedAudio;
}

export interface CanvasSpec {
  width: number;
  height: number;
  aspect_ratio: string;
  fps: number;
  background_color?: string;
  safe_area?: SafeArea;
}

export interface CTASpec {
  text?: string;
  placement?: TextPlacement;
  style?: TypographyStyle | string;
}

export interface BrandingSpec {
  name?: string;
  primary_color?: string;
  secondary_color?: string;
  logo_asset_id?: string;
  logo_path?: string;
  font_preference?: string;
}

export interface CompositionMediaAsset {
  asset_id: string;
  media_type: MediaType;
  source_path: string;
  prepared_path?: string;
  width?: number;
  height?: number;
  duration_seconds?: number;
}

export interface TimelineScene extends TimelineClip {
  id: string;
  media: CompositionMediaAsset;
}

export interface CompositionTransition {
  scene_id: string;
  type: TransitionType;
  duration_frames: number;
}

export interface CompositionEffect {
  scene_id: string;
  type: string;
  parameters?: Record<string, unknown>;
}

export interface ResolvedAudio {
  source: string;
  original_audio: boolean;
  original_volume: number;
}

export interface RenderResult {
  record: RenderRecord;
  composition: CompositionSpec;
}
