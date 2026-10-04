import React from 'react';
import {AbsoluteFill, Audio, Composition, Img, OffthreadVideo, Sequence, interpolate, registerRoot, staticFile, useCurrentFrame, useVideoConfig} from 'remotion';
import type {CaptionStyle, CompositionSpec, ResolvedAudio, StoryCaption, TextOverlay, TimelineBRoll, TimelineClip} from '../types.js';

interface StoryProps extends Record<string, unknown> {
  composition: CompositionSpec;
}

const emptyComposition: CompositionSpec = {
  renderer_version: 'framemio-renderer-v1',
  project_type: 'story',
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
  socialTextColor: '#FFD60A',
  strokeColor: '#000000',
  strokeWidth: 3,
  backgroundColor: 'rgba(0,0,0,0.32)',
  bottomOffset: 150,
  maxWidth: 0.82,
};

const captionFontFamilies: Record<string, string> = {
  inter: 'Noto Sans, Arial, sans-serif',
  'inter-bold': 'Noto Sans, Arial, sans-serif',
  system: 'Noto Sans, Arial, sans-serif',
  'system-bold': 'Noto Sans, Arial, sans-serif',
  'noto-sans': 'Noto Sans, Arial, sans-serif',
  'noto-sans-bold': 'Noto Sans, Arial, sans-serif',
};

export const FrameMioStory: React.FC<StoryProps> = ({composition}) => {
  const {fps} = useVideoConfig();
  return (
    <AbsoluteFill style={{backgroundColor: '#0f0f12', overflow: 'hidden'}}>
      {composition.clips.map((clip, index) => (
        <Sequence key={clip.clip_id} from={clip.start_frame} durationInFrames={clip.duration_frames}>
          <MomentLayer clip={composition.scenes?.[index] ?? clip} index={index} clips={composition.scenes ?? composition.clips} fps={fps} />
        </Sequence>
      ))}
      {composition.broll.map((overlay) => (
        <Sequence key={overlay.broll_id} from={overlay.start_frame} durationInFrames={overlay.duration_frames}>
          <BRollLayer overlay={overlay} />
        </Sequence>
      ))}
      <BrandLayer composition={composition} />
      <CTALayer composition={composition} fps={fps} />
      {composition.captions.map((caption, index) => <CaptionLayer key={`${caption.moment_id}-${index}`} caption={caption} styleSpec={composition.caption_style} fps={fps} />)}
      <AudioLayer audio={composition.audio} clips={composition.clips} />
    </AbsoluteFill>
  );
};

const MomentLayer: React.FC<{clip: TimelineClip; index: number; clips: TimelineClip[]; fps: number}> = ({clip, index, clips, fps}) => {
  const opacity = useTransitionOpacity(clip, index, clips, fps);
  const style: React.CSSProperties = {opacity};
  return (
    <AbsoluteFill style={style}>
      {clip.media_type === 'video'
        ? <OffthreadVideo src={sourceURL(clip.prepared_path)} muted={clip.volume <= 0} volume={clip.volume} style={mediaStyle(clip.crop_intent)} />
        : <PhotoLayer clip={clip} />}
      {(clip.overlays?.length ? clip.overlays : clip.typography ? [clip.typography] : []).map((overlay, overlayIndex) => (
        <TypographyLayer key={`${clip.clip_id}-overlay-${overlayIndex}`} typography={overlay} durationFrames={clip.duration_frames} fps={fps} />
      ))}
    </AbsoluteFill>
  );
};

function useTransitionOpacity(clip: TimelineClip, index: number, clips: TimelineClip[], fps: number): number {
  const frame = useCurrentFrame();
  const fadeFrames = fadeDurationFrames(clip, fps);
  if (fadeFrames <= 0) {
    return 1;
  }
  const fadeIn = index > 0 && isFadeTransition(clip.transition)
    ? interpolate(frame, [0, fadeFrames], [0, 1], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'})
    : 1;
  const nextClip = clips[index + 1];
  const fadeOutStart = Math.max(0, clip.duration_frames - fadeFrames);
  const fadeOut = nextClip && isFadeTransition(nextClip.transition)
    ? interpolate(frame, [fadeOutStart, clip.duration_frames], [1, 0], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'})
    : 1;
  return Math.min(fadeIn, fadeOut);
}

function isFadeTransition(transition?: string): boolean {
  return transition === 'fade' || transition === 'dissolve' || transition === 'crossfade';
}

function fadeDurationFrames(clip: TimelineClip, fps: number): number {
  const fadeSeconds = Math.max(0.001, Math.min(0.45, Math.max(0.001, clip.duration_ms / 3000)));
  return Math.max(1, Math.round(fadeSeconds * fps));
}

const BRollLayer: React.FC<{overlay: TimelineBRoll}> = ({overlay}) => {
  const frame = useCurrentFrame();
  const {fps} = useVideoConfig();
  if (overlay.generated_by === 'hyperframes') {
    return (
      <AbsoluteFill style={{pointerEvents: 'none'}}>
        <OffthreadVideo src={sourceURL(overlay.prepared_path)} muted style={{width: '100%', height: '100%', objectFit: 'contain'}} />
      </AbsoluteFill>
    );
  }
  const opacity = overlay.animation === 'fade'
    ? fadeValue(frame, overlay.duration_frames, fps)
    : overlay.opacity;
  const entranceFrames = Math.max(1, Math.round(0.4 * fps));
  const progress = interpolate(frame, [0, entranceFrames], [0, 1], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'});
  const scale = overlay.animation === 'scale'
    ? interpolate(progress, [0, 1], [0.82, 1])
    : overlay.animation === 'subtle_scale'
      ? interpolate(progress, [0, 1], [0.96, 1])
      : 1;
  const positionOffset = overlay.animation === 'move' ? 5 * (1 - progress) : 0;
  const basePlacement = overlayPlacementStyle(overlay);
  const transform = [
    typeof basePlacement.transform === 'string' ? basePlacement.transform : '',
    overlay.animation === 'move' ? `translate(${moveOffsetX(overlay, positionOffset)}%, ${moveOffsetY(overlay, positionOffset)}%)` : '',
    `scale(${scale})`,
  ].filter(Boolean).join(' ');
  const content = overlay.media_type === 'video'
    ? <OffthreadVideo src={sourceURL(overlay.prepared_path)} muted style={mediaStyle(overlay.crop_intent)} />
    : <PhotoLayer clip={overlay} />;
  if (overlay.presentation === 'full_frame') {
    return <AbsoluteFill style={{opacity}}>{content}</AbsoluteFill>;
  }
  return (
    <AbsoluteFill style={{pointerEvents: 'none'}}>
      <div style={{...basePlacement, opacity, transform, border: overlayFrameStyle(overlay.frame), overflow: 'hidden'}}>
        {content}
      </div>
    </AbsoluteFill>
  );
};

function moveOffsetX(overlay: TimelineBRoll, amount: number): number {
  if (overlay.placement === 'top_left' || overlay.placement === 'bottom_left' || overlay.placement === 'left') return -amount;
  if (overlay.placement === 'top_right' || overlay.placement === 'bottom_right' || overlay.placement === 'right') return amount;
  return 0;
}

function moveOffsetY(overlay: TimelineBRoll, amount: number): number {
  return overlay.placement === 'center' ? amount : 0;
}

function fadeValue(frame: number, durationFrames: number, fps: number): number {
  const fadeFrames = Math.max(1, Math.round(0.35 * fps));
  return Math.min(
    interpolate(frame, [0, fadeFrames], [0, 1], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'}),
    interpolate(frame, [Math.max(0, durationFrames - fadeFrames), durationFrames], [1, 0], {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'}),
  );
}

function overlayPlacementStyle(overlay: TimelineBRoll): React.CSSProperties {
  const size = overlaySize(overlay.size);
  const style: React.CSSProperties = {
    position: 'absolute',
    width: size.width,
    height: size.height,
    backgroundColor: '#111',
  };
  switch (overlay.placement) {
    case 'top_left':
      style.top = '5%'; style.left = '5%'; break;
    case 'top_right':
      style.top = '5%'; style.right = '5%'; break;
    case 'bottom_left':
      style.bottom = '8%'; style.left = '5%'; break;
    case 'center':
      style.top = '50%'; style.left = '50%'; style.transform = 'translate(-50%, -50%)'; break;
    case 'left':
      style.top = '0'; style.bottom = '0'; style.left = '0'; break;
    case 'right':
      style.top = '0'; style.bottom = '0'; style.right = '0'; break;
    default:
      style.bottom = '8%'; style.right = '5%';
  }
  return style;
}

function overlaySize(size: TimelineBRoll['size']): {width: string; height: string} {
  switch (size) {
    case 'small': return {width: '28%', height: '28%'};
    case 'large': return {width: '52%', height: '52%'};
    case 'half': return {width: '50%', height: '100%'};
    default: return {width: '38%', height: '38%'};
  }
}

function overlayFrameStyle(frame: TimelineBRoll['frame']): string {
  if (frame === 'bold') return '6px solid rgba(255,255,255,0.92)';
  if (frame === 'subtle') return '3px solid rgba(255,255,255,0.72)';
  return 'none';
}

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
  const presentation = captionPresentation(styleSpec);
  const displayText = presentation === 'word_by_word' ? activeCaptionPhrase(text, frame, start, end) : text;
  if (!displayText) {
    return null;
  }
  const fontSize = clamp(styleSpec?.font_size ?? captionDefaults.fontSize, 20, presentation === 'word_by_word' ? 112 : 88);
  const bottom = clamp(styleSpec?.bottom_offset ?? captionDefaults.bottomOffset, 40, 420);
  const color = styleSpec?.text_color ?? (presentation === 'word_by_word' ? captionDefaults.socialTextColor : captionDefaults.textColor);
  const maxWidth = presentation === 'word_by_word' ? '92%' : `${captionDefaults.maxWidth * 100}%`;
  const horizontalPadding = presentation === 'word_by_word' ? '10px 20px' : '12px 22px';
  return (
    <AbsoluteFill style={{justifyContent: 'flex-end', alignItems: 'center', paddingBottom: bottom, pointerEvents: 'none'}}>
      <div style={{maxWidth, padding: horizontalPadding, borderRadius: 16, background: styleSpec?.background_enabled === false ? 'transparent' : captionDefaults.backgroundColor, color, fontFamily: captionFontFamily(styleSpec?.font_family), fontSize, fontWeight: 800, lineHeight: 1.02, textAlign: 'center', textTransform: 'none', WebkitTextStroke: `${clamp(styleSpec?.stroke_width ?? captionDefaults.strokeWidth, 0, 8)}px ${styleSpec?.stroke_color ?? captionDefaults.strokeColor}`, textShadow: '0 3px 12px rgba(0,0,0,0.55)'}}>{displayText}</div>
    </AbsoluteFill>
  );
};

function captionFontFamily(value?: string): string {
  return captionFontFamilies[value?.trim().toLowerCase() ?? ''] ?? captionFontFamilies.inter;
}

function captionPresentation(styleSpec?: CaptionStyle): 'full_text' | 'word_by_word' {
  const value = styleSpec?.presentation;
  if (value === 'word_by_word') {
    return value;
  }
  return 'full_text';
}

function activeCaptionPhrase(text: string, frame: number, startFrame: number, endFrame: number): string {
  const words = text.split(/\s+/).map((word) => word.trim()).filter(Boolean);
  if (words.length === 0) {
    return '';
  }
  const chunks = captionPhraseChunks(words);
  if (chunks.length === 1 || endFrame <= startFrame) {
    return chunks[0].join(' ');
  }
  const progress = clamp((frame - startFrame) / Math.max(1, endFrame - startFrame), 0, 0.999999);
  return chunks[Math.min(chunks.length - 1, Math.floor(progress * chunks.length))]?.join(' ') ?? chunks[0].join(' ');
}

function captionPhraseChunks(words: string[]): string[][] {
  const chunks: string[][] = [];
  let current: string[] = [];
  for (const word of words) {
    const next = [...current, word];
    if (current.length > 0 && (next.length > 3 || next.join(' ').length > 18)) {
      chunks.push(current);
      current = [word];
      continue;
    }
    current = next;
  }
  if (current.length > 0) {
    chunks.push(current);
  }
  return chunks.length > 0 ? chunks : [words];
}

const BrandLayer: React.FC<{composition: CompositionSpec}> = ({composition}) => {
  const maybeBrand = composition.branding;
  if (!maybeBrand || (!maybeBrand.logo_path && !cleanText(maybeBrand.name))) {
    return null;
  }
  const safe = composition.canvas?.safe_area ?? {top: 0.06, right: 0.06, bottom: 0.08, left: 0.06};
  return (
    <AbsoluteFill style={{pointerEvents: 'none'}}>
      <div style={{position: 'absolute', top: percent(safe.top ?? 0.06), left: percent(safe.left ?? 0.06), display: 'flex', alignItems: 'center', gap: 12, maxWidth: '72%'}}>
        {maybeBrand.logo_path ? <Img src={sourceURL(maybeBrand.logo_path)} style={{width: 56, height: 56, objectFit: 'contain', borderRadius: 8}} /> : null}
        {cleanText(maybeBrand.name) ? <div style={{fontFamily: 'Liberation Sans, Arial, sans-serif', fontSize: 28, fontWeight: 700, color: maybeBrand.primary_color ?? '#FFFFFF', textShadow: '0 3px 12px rgba(0,0,0,0.48)'}}>{cleanText(maybeBrand.name)}</div> : null}
      </div>
    </AbsoluteFill>
  );
};

const CTALayer: React.FC<{composition: CompositionSpec; fps: number}> = ({composition, fps}) => {
  const text = cleanText(composition.cta?.text ?? composition.call_to_action);
  if (!text || composition.clips.length === 0) {
    return null;
  }
  const last = composition.clips[composition.clips.length - 1];
  const durationFrames = Math.max(1, Math.min(last.duration_frames, Math.round(2.4 * fps)));
  const from = Math.max(0, last.start_frame + last.duration_frames - durationFrames);
  const typography: TextOverlay = {
    enabled: true,
    title: text,
    style: String(composition.cta?.style ?? 'editorial'),
    placement: String(composition.cta?.placement ?? 'bottom_center'),
    start_ms: 0,
    duration_ms: Math.round((durationFrames / fps) * 1000),
    max_width: 0.78,
    safe_area: composition.canvas?.safe_area,
    animation: 'fade_up',
  };
  return (
    <Sequence from={from} durationInFrames={durationFrames}>
      <TypographyLayer typography={typography} durationFrames={durationFrames} fps={fps} />
    </Sequence>
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
