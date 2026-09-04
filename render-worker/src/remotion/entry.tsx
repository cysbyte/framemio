import React from 'react';
import {AbsoluteFill, Audio, Composition, Img, OffthreadVideo, Sequence, interpolate, registerRoot, staticFile, useCurrentFrame, useVideoConfig} from 'remotion';
import type {CaptionStyle, CompositionSpec, ResolvedAudio, StoryCaption, TextOverlay, TimelineBRoll, TimelineClip} from '../types.js';

interface StoryProps extends Record<string, unknown> {
  composition: CompositionSpec;
}

const emptyComposition: CompositionSpec = {
  renderer_version: 'framemio-renderer-v1',
  story_plan_id: 'empty',
  story_plan_version: 1,
  project_id: 'empty',
  fps: 30,
  resolution: {width: 1080, height: 1920},
  duration_frames: 30,
  duration_ms: 1000,
  clips: [],
  broll: [],
  captions: [],
  audio: {source: 'none', original_audio: false, original_volume: 0},
};

const typographyPresets = {
  cinematic: {family: 'Liberation Serif, Georgia, serif', title: 66, subtitle: 34, weight: 500, letterSpacing: 1.4, shadow: '0 4px 20px rgba(0,0,0,0.55)'},
  minimal: {family: 'Liberation Sans, Arial, sans-serif', title: 48, subtitle: 28, weight: 500, letterSpacing: 0.2, shadow: '0 3px 14px rgba(0,0,0,0.45)'},
  editorial: {family: 'Liberation Serif, Georgia, serif', title: 76, subtitle: 34, weight: 600, letterSpacing: 0.5, shadow: '0 5px 22px rgba(0,0,0,0.50)'},
  memory: {family: 'Liberation Sans, Arial, sans-serif', title: 54, subtitle: 30, weight: 500, letterSpacing: 0.1, shadow: '0 4px 18px rgba(0,0,0,0.52)'},
  location: {family: 'Liberation Sans, Arial, sans-serif', title: 36, subtitle: 24, weight: 600, letterSpacing: 2.2, shadow: '0 3px 12px rgba(0,0,0,0.48)'},
} as const;

const captionDefaults = {
  fontSize: 44,
  textColor: '#FFFFFF',
  strokeColor: '#000000',
  strokeWidth: 3,
  backgroundColor: 'rgba(0,0,0,0.32)',
  bottomOffset: 150,
  maxWidth: 0.82,
};

export const FrameMioStory: React.FC<StoryProps> = ({composition}) => {
  const {fps} = useVideoConfig();
  return (
    <AbsoluteFill style={{backgroundColor: '#0f0f12', overflow: 'hidden'}}>
      {composition.clips.map((clip) => (
        <Sequence key={clip.clip_id} from={clip.start_frame} durationInFrames={clip.duration_frames}>
          <MomentLayer clip={clip} />
        </Sequence>
      ))}
      {composition.broll.map((overlay) => (
        <Sequence key={overlay.broll_id} from={overlay.start_frame} durationInFrames={overlay.duration_frames}>
          <BRollLayer overlay={overlay} />
        </Sequence>
      ))}
      {composition.captions.map((caption, index) => <CaptionLayer key={`${caption.moment_id}-${index}`} caption={caption} styleSpec={composition.caption_style} fps={fps} />)}
      <AudioLayer audio={composition.audio} clips={composition.clips} />
    </AbsoluteFill>
  );
};

const MomentLayer: React.FC<{clip: TimelineClip}> = ({clip}) => {
  if (clip.media_type === 'video') {
    return <OffthreadVideo src={sourceURL(clip.prepared_path)} muted={clip.volume <= 0} volume={clip.volume} style={mediaStyle(clip.crop_intent)} />;
  }
  return <PhotoLayer clip={clip} />;
};

const BRollLayer: React.FC<{overlay: TimelineBRoll}> = ({overlay}) => {
  if (overlay.media_type === 'video') {
    return <OffthreadVideo src={sourceURL(overlay.prepared_path)} muted style={mediaStyle(overlay.crop_intent)} />;
  }
  return <PhotoLayer clip={overlay} />;
};

const PhotoLayer: React.FC<{clip: TimelineClip | TimelineBRoll}> = ({clip}) => {
  const frame = useCurrentFrame();
  const progress = clip.duration_frames <= 1 ? 1 : frame / (clip.duration_frames - 1);
  const transform = clip.transform.kind === 'photo' ? clip.transform : undefined;
  const scale = transform ? interpolate(progress, [0, 1], [transform.from_scale, transform.to_scale]) : 1.04;
  const x = transform ? interpolate(progress, [0, 1], [transform.from_x, transform.to_x]) : 0;
  const y = transform ? interpolate(progress, [0, 1], [transform.from_y, transform.to_y]) : 0;
  return (
    <AbsoluteFill style={{overflow: 'hidden', backgroundColor: '#111'}}>
      <Img src={sourceURL(clip.prepared_path)} style={{...mediaStyle(clip.crop_intent), transform: `scale(${scale}) translate(${x * 100}%, ${y * 100}%)`}} />
    </AbsoluteFill>
  );
};

const TypographyLayer: React.FC<{typography: TextOverlay; durationFrames: number; fps: number}> = ({typography, durationFrames, fps}) => {
  const frame = useCurrentFrame();
  const start = Math.max(0, Math.round(((typography.start_ms ?? 350) / 1000) * fps));
  const requested = Math.max(1, Math.round(((typography.duration_ms ?? 2500) / 1000) * fps));
  const end = Math.min(durationFrames, start + requested);
  if (frame < start || frame > end) {
    return null;
  }
  const local = frame - start;
  const remaining = Math.max(0, end - frame);
  const fadeIn = Math.max(1, Math.round(0.45 * fps));
  const fadeOut = Math.max(1, Math.round(0.35 * fps));
  const opacity = Math.min(interpolate(local, [0, fadeIn], [0, 1], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'}), interpolate(remaining, [0, fadeOut], [0, 1], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'}));
  const animation = typography.animation ?? 'fade_up';
  const translateY = animation === 'fade_up'
    ? interpolate(local, [0, fadeIn], [18, 0], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'})
    : animation === 'fade_down'
      ? interpolate(local, [0, fadeIn], [-18, 0], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'})
      : 0;
  const scale = animation === 'subtle_scale' ? interpolate(local, [0, fadeIn], [0.985, 1], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'}) : 1;
  const preset = typographyPresets[supportedTypographyStyle(typography.style)];
  const maxWidth = `${Math.round(clamp(typography.max_width ?? 0.72, 0.2, 0.95) * 100)}%`;
  const placement = placementStyle(typography);
  const baseTransform = typeof placement.transform === 'string' ? placement.transform : '';
  const transform = [baseTransform, `translateY(${translateY}px) scale(${scale})`].filter(Boolean).join(' ');
  return (
    <AbsoluteFill style={{pointerEvents: 'none'}}>
      <div style={{...placement, maxWidth, opacity, transform}}>
        <div style={{padding: readabilityPadding(typography), borderRadius: 18, background: readabilityBackground(typography)}}>
          {cleanText(typography.title) ? <div style={{fontFamily: preset.family, fontSize: preset.title, fontWeight: preset.weight, letterSpacing: preset.letterSpacing, color: '#fff', lineHeight: 1.04, textShadow: preset.shadow}}>{cleanText(typography.title)}</div> : null}
          {cleanText(typography.subtitle) ? <div style={{fontFamily: preset.family, fontSize: preset.subtitle, fontWeight: 400, letterSpacing: 0.1, color: 'rgba(255,255,255,0.92)', lineHeight: 1.18, marginTop: 12, textShadow: preset.shadow}}>{cleanText(typography.subtitle)}</div> : null}
        </div>
      </div>
    </AbsoluteFill>
  );
};

const CaptionLayer: React.FC<{caption: StoryCaption; styleSpec?: CaptionStyle; fps: number}> = ({caption, styleSpec, fps}) => {
  const frame = useCurrentFrame();
  const start = Math.max(0, Math.round(((caption.start_ms ?? 0) / 1000) * fps));
  const end = Math.max(start + 1, Math.round(((caption.end_ms ?? 0) / 1000) * fps));
  if (frame < start || frame > end) {
    return null;
  }
  const text = cleanText(caption.text);
  if (!text) {
    return null;
  }
  const fontSize = clamp(styleSpec?.font_size ?? captionDefaults.fontSize, 20, 88);
  const bottom = clamp(styleSpec?.bottom_offset ?? captionDefaults.bottomOffset, 40, 420);
  return (
    <AbsoluteFill style={{justifyContent: 'flex-end', alignItems: 'center', paddingBottom: bottom, pointerEvents: 'none'}}>
      <div style={{maxWidth: `${captionDefaults.maxWidth * 100}%`, padding: '12px 22px', borderRadius: 16, background: styleSpec?.background_enabled === false ? 'transparent' : captionDefaults.backgroundColor, color: styleSpec?.text_color ?? captionDefaults.textColor, fontFamily: 'Liberation Sans, Arial, sans-serif', fontSize, fontWeight: 700, lineHeight: 1.12, textAlign: 'center', WebkitTextStroke: `${clamp(styleSpec?.stroke_width ?? captionDefaults.strokeWidth, 0, 8)}px ${styleSpec?.stroke_color ?? captionDefaults.strokeColor}`, textShadow: '0 3px 12px rgba(0,0,0,0.55)'}}>{text}</div>
    </AbsoluteFill>
  );
};

const AudioLayer: React.FC<{audio: ResolvedAudio; clips: TimelineClip[]}> = ({audio, clips}) => {
  if (!audio.original_audio) {
    return null;
  }
  return <>{clips.filter((clip) => clip.media_type === 'video' && clip.volume > 0).map((clip) => <Sequence key={`audio-${clip.clip_id}`} from={clip.start_frame} durationInFrames={clip.duration_frames}><Audio src={sourceURL(clip.prepared_path)} volume={Math.min(1, Math.max(0, clip.volume * audio.original_volume))} /></Sequence>)}</>;
};

const Root: React.FC = () => (
  <Composition
    id="FramemioStory"
    component={FrameMioStory}
    durationInFrames={emptyComposition.duration_frames}
    fps={emptyComposition.fps}
    width={emptyComposition.resolution.width}
    height={emptyComposition.resolution.height}
    defaultProps={{composition: emptyComposition}}
    calculateMetadata={({props}) => ({
      durationInFrames: Math.max(1, props.composition.duration_frames),
      fps: Math.max(1, props.composition.fps),
      width: Math.max(1, props.composition.resolution.width),
      height: Math.max(1, props.composition.resolution.height),
    })}
  />
);

function sourceURL(filePath: string): string {
  if (filePath.startsWith('remotion-static:')) {
    return staticFile(filePath.slice('remotion-static:'.length));
  }
  if (/^[a-z]+:\/\//i.test(filePath)) {
    return filePath;
  }
  return `file://${filePath}`;
}

function mediaStyle(cropIntent?: string): React.CSSProperties {
  return {width: '100%', height: '100%', objectFit: cropIntent === 'fit' ? 'contain' : 'cover'};
}

function placementStyle(typography: TextOverlay): React.CSSProperties {
  const safe = typography.safe_area ?? {};
  const vertical = placementParts(typography.placement)[0];
  const horizontal = placementParts(typography.placement)[1];
  const style: React.CSSProperties = {position: 'absolute', whiteSpace: 'pre-wrap'};
  if (vertical === 'top') {
    style.top = percent(safe.top ?? 0.08);
  } else if (vertical === 'center') {
    style.top = '50%';
  } else {
    style.bottom = percent(safe.bottom ?? 0.08);
  }
  if (horizontal === 'left') {
    style.left = percent(safe.left ?? 0.08);
    style.textAlign = 'left';
  } else if (horizontal === 'right') {
    style.right = percent(safe.right ?? 0.08);
    style.textAlign = 'right';
  } else {
    style.left = '50%';
    style.textAlign = 'center';
  }
  const transforms = [];
  if (vertical === 'center') {
    transforms.push('translateY(-50%)');
  }
  if (horizontal === 'center') {
    transforms.push('translateX(-50%)');
  }
  if (transforms.length > 0) {
    style.transform = transforms.join(' ');
  }
  return style;
}

function placementParts(value?: string): [string, string] {
  const placement = value && ['top_left', 'top_center', 'top_right', 'center_left', 'center', 'center_right', 'bottom_left', 'bottom_center', 'bottom_right'].includes(value) ? value : 'bottom_left';
  if (placement === 'center') {
    return ['center', 'center'];
  }
  const [vertical, horizontal] = placement.split('_');
  return [vertical, horizontal];
}

function supportedTypographyStyle(value?: string): keyof typeof typographyPresets {
  return value && value in typographyPresets ? value as keyof typeof typographyPresets : 'cinematic';
}

function readabilityPadding(typography: TextOverlay): string {
  return typography.style === 'editorial' ? '16px 20px' : '4px 0';
}

function readabilityBackground(typography: TextOverlay): string {
  if (typography.style === 'editorial') {
    return 'linear-gradient(90deg, rgba(0,0,0,0.28), rgba(0,0,0,0.03))';
  }
  return 'linear-gradient(90deg, rgba(0,0,0,0.20), rgba(0,0,0,0.00))';
}

function percent(value: number): string {
  return `${Math.round(clamp(value, 0, 0.5) * 10000) / 100}%`;
}

function cleanText(value?: string): string {
  return (value ?? '').replace(/[\u0000-\u001f\u007f]/g, ' ').replace(/\s+/g, ' ').trim();
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, Number.isFinite(value) ? value : min));
}

registerRoot(Root);
