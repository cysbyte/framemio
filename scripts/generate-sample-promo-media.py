#!/usr/bin/env python3
"""Generate FrameMio Promo test media using the configured image and video APIs.

The generated set is a short product-promo sequence:

HOOK -> PROBLEM -> PRODUCT -> BENEFIT -> PROOF -> CTA

Video moments request spoken English voiceover and are rejected when the
downloaded MP4 does not contain an audio stream. Exact text overlays are
intentionally omitted from generated media because Promo typography is added
deterministically by the renderer.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Literal


REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_ENV_FILE = REPO_ROOT.parent / "tokenduoduo" / "gateway" / ".env"
DEFAULT_OUTPUT_DIR = REPO_ROOT / "sample-media" / "promo-framemio"
DEFAULT_IMAGE_MODEL = "gpt-image-2"
DEFAULT_IMAGE_SIZE = "1024x1024"
DEFAULT_IMAGE_QUALITY = "high"
DEFAULT_IMAGE_FORMAT = "png"
DEFAULT_VIDEO_MODEL = "grok-imagine-video"
DEFAULT_VIDEO_ASPECT_RATIO = "16:9"
DEFAULT_VIDEO_RESOLUTION = "720p"
DEFAULT_POLL_INTERVAL = 5
DEFAULT_TIMEOUT = 900

Kind = Literal["photo", "video"]

PROMO_STYLE_PROMPT = """Create synthetic photoreal media for a FrameMio product-promo test.
FrameMio is an approachable AI video app that turns selected photos and videos
into finished promotional videos. The media should look like polished,
credible product marketing footage, not generic stock footage.

Use a warm, premium consumer-tech visual language: cream surfaces, muted coral,
soft rose, warm brown, clean daylight, subtle cinematic contrast, and realistic
phone-camera texture. Show synthetic, non-identifiable people only. Keep faces
turned away, in profile, cropped, or softly out of focus.

Do not generate readable text, captions, UI copy, logos, watermarks, trademarks,
brand names, fake app labels, or exact marketing typography. Do not create
distorted hands, extra fingers, malformed phones, or impossible screens.
Product and phone screens may show abstract, legible-at-a-glance editing
interfaces without readable words; final Promo text is rendered by FrameMio.
"""


@dataclass(frozen=True)
class PromoAsset:
    slug: str
    title: str
    stage: str
    kind: Kind
    duration_seconds: int
    prompt: str
    voiceover: str = ""


PROMO_ASSETS: list[PromoAsset] = [
    PromoAsset(
        slug="01-hook-camera-roll",
        title="Camera Roll Hook",
        stage="hook",
        kind="photo",
        duration_seconds=3,
        prompt="""A premium overhead product-lifestyle photograph: a modern smartphone,
several printed-looking photo thumbnails, and a few camera-roll video frames
spread across a warm cream desk, with one phone centered and ready to organize
the media. Strong visual opening, clear subject hierarchy, no readable screen
copy, no logos.""",
    ),
    PromoAsset(
        slug="02-problem-footage",
        title="Too Much Footage",
        stage="problem",
        kind="video",
        duration_seconds=5,
        voiceover="Your camera roll is full of great moments, but turning them into a finished video takes time.",
        prompt="""A 5-second polished product-marketing video in a bright home workspace:
an overwhelmed creator scrolls through a large collection of photos and short
video clips on a smartphone, pauses, and looks at the unorganized media on the
desk. Use a gentle push-in and natural hand movement. Keep the phone screen
abstract with no readable words. Include a clear, natural English spoken
voiceover saying exactly: "Your camera roll is full of great moments, but
turning them into a finished video takes time." Include quiet room ambience,
no music overpowering the voice, no captions.""",
    ),
    PromoAsset(
        slug="03-product-ai-demo",
        title="AI Finds the Story",
        stage="product",
        kind="video",
        duration_seconds=7,
        voiceover="FrameMio uses AI to find the moments that matter and shape them into a polished video.",
        prompt="""A 7-second polished product-marketing video in the same bright workspace:
a creator selects a mix of travel photos and phone videos, then the phone
transitions into an abstract, elegant AI-assisted editing flow with meaningful
moments grouped into a clean sequence. Show the creator becoming confident as
the edit takes shape. Keep all interface text unreadable or absent. Include a
clear, natural English spoken voiceover saying exactly: "FrameMio uses AI to
find the moments that matter and shape them into a polished video." Include
soft device taps and room ambience, no music overpowering the voice, no
captions.""",
    ),
    PromoAsset(
        slug="04-benefit-finished-video",
        title="Finished Video",
        stage="benefit",
        kind="photo",
        duration_seconds=4,
        prompt="""A clean premium product photograph: a smartphone standing on a warm
cream tabletop, showing an abstract finished vertical video with a beautiful
travel moment, while a relaxed creator's hand reaches toward the share action.
The result should communicate clarity, polish, and ease. No readable UI copy,
logos, or generated text; screen content must remain abstract but visibly
finished.""",
    ),
    PromoAsset(
        slug="05-proof-ready-to-share",
        title="Ready to Share",
        stage="proof",
        kind="video",
        duration_seconds=6,
        voiceover="From raw photos and clips to a video you can share in minutes.",
        prompt="""A 6-second polished product-marketing video: a finished short video plays
on a smartphone, the camera moves from the phone to a small group of synthetic
friends viewing it together and reacting warmly, then returns to the phone
ready to share. Keep faces non-identifiable and the screen free of readable
copy. Include a clear, natural English spoken voiceover saying exactly: "From
raw photos and clips to a video you can share in minutes." Include subtle
natural room ambience, no music overpowering the voice, no captions.""",
    ),
    PromoAsset(
        slug="06-cta-give-it-footage",
        title="Give FrameMio Your Footage",
        stage="cta",
        kind="video",
        duration_seconds=4,
        voiceover="Give FrameMio your footage and get a finished video.",
        prompt="""A 4-second polished closing product-marketing video: a smartphone and a
small set of photos and video thumbnails rest on a clean warm desk, the phone
shows an abstract completed film and a confident hand picks it up to share.
End on a calm, decisive product shot with generous negative space for a
renderer-added CTA. Include a clear, natural English spoken voiceover saying
exactly: "Give FrameMio your footage and get a finished video." No readable
screen text, no captions, no logos, no music overpowering the voice.""",
    ),
]

TERMINAL_STATUSES = {
    "completed",
    "succeeded",
    "success",
    "failed",
    "cancelled",
    "canceled",
    "error",
}
SUCCESS_STATUSES = {"completed", "succeeded", "success"}


def load_env_file(path: Path) -> None:
    if not path.is_file():
        return
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        if line.startswith("export "):
            line = line[len("export ") :].strip()
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        if key and key not in os.environ:
            os.environ[key] = value


def normalize_images_url(base_url: str) -> str:
    cleaned = base_url.rstrip("/")
    if cleaned.endswith("/images/generations"):
        return cleaned
    if cleaned.endswith("/v1"):
        return f"{cleaned}/images/generations"
    return f"{cleaned}/v1/images/generations"


def normalize_video_base_url(base_url: str) -> str:
    cleaned = base_url.rstrip("/")
    if cleaned.endswith("/videos"):
        return cleaned[: -len("/videos")]
    return cleaned


def video_create_url(base_url: str) -> str:
    cleaned = normalize_video_base_url(base_url)
    if cleaned.endswith("/v1"):
        return f"{cleaned}/videos"
    return f"{cleaned}/v1/videos"


def video_status_url(base_url: str, video_id: str) -> str:
    return f"{video_create_url(base_url)}/{urllib.parse.quote(video_id, safe='')}"


def request_json(
    url: str,
    api_key: str,
    payload: dict[str, Any] | None = None,
    method: str = "GET",
) -> dict[str, Any]:
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=data,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Accept": "application/json",
            **({"Content-Type": "application/json"} if payload is not None else {}),
        },
        method=method,
    )
    try:
        with urllib.request.urlopen(request, timeout=300) as response:
            body = response.read().decode("utf-8")
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Request failed: HTTP {error.code}\n{body}") from error
    except urllib.error.URLError as error:
        raise RuntimeError(f"Request failed: {error.reason}") from error


def find_string(payload: Any, keys: set[str]) -> str:
    if isinstance(payload, dict):
        for key, value in payload.items():
            normalized = key.lower().replace("-", "_")
            if normalized in keys and isinstance(value, str) and value.strip():
                return value.strip()
        for value in payload.values():
            found = find_string(value, keys)
            if found:
                return found
    elif isinstance(payload, list):
        for value in payload:
            found = find_string(value, keys)
            if found:
                return found
    return ""


def find_video_id(payload: dict[str, Any]) -> str:
    return find_string(payload, {"id", "video_id", "task_id", "job_id"})


def find_video_url(payload: dict[str, Any]) -> str:
    return find_string(
        payload,
        {"video_url", "download_url", "url", "output_url", "file_url", "content_url"},
    )


def payload_status(payload: dict[str, Any]) -> str:
    value = payload.get("status")
    return str(value).strip().lower() if value is not None else ""


def payload_progress(payload: dict[str, Any]) -> str:
    value = payload.get("progress")
    return "" if value is None else str(value)


def same_provider_url(url: str, base_url: str) -> bool:
    resolved = urllib.parse.urljoin(base_url.rstrip("/") + "/", url)
    return url.startswith("/") or (
        urllib.parse.urlparse(resolved).netloc
        == urllib.parse.urlparse(base_url).netloc
    )


def save_image(
    result: dict[str, Any],
    output_path: Path,
    base_url: str,
    api_key: str,
) -> None:
    data = result.get("data")
    if not isinstance(data, list) or not data:
        raise RuntimeError(
            f"No image data returned: {json.dumps(result, ensure_ascii=False)[:1000]}"
        )
    first = data[0]
    if not isinstance(first, dict):
        raise RuntimeError(f"Unexpected image response item: {first!r}")

    output_path.parent.mkdir(parents=True, exist_ok=True)
    encoded_image = first.get("b64_json")
    if isinstance(encoded_image, str) and encoded_image:
        output_path.write_bytes(base64.b64decode(encoded_image))
        return

    image_url = first.get("url")
    if isinstance(image_url, str) and image_url:
        resolved_url = urllib.parse.urljoin(base_url.rstrip("/") + "/", image_url)
        headers = {"Accept": "image/*,*/*"}
        if api_key and same_provider_url(image_url, base_url):
            headers["Authorization"] = f"Bearer {api_key}"
        request = urllib.request.Request(resolved_url, headers=headers)
        with urllib.request.urlopen(request, timeout=300) as response:
            output_path.write_bytes(response.read())
        return

    raise RuntimeError(
        f"Image response has neither b64_json nor url: {json.dumps(first, ensure_ascii=False)[:1000]}"
    )


def save_video_from_payload(
    payload: dict[str, Any],
    output_path: Path,
    base_url: str,
    api_key: str,
) -> bool:
    b64_video = find_string(payload, {"b64_json", "base64", "video_base64"})
    if b64_video:
        output_path.parent.mkdir(parents=True, exist_ok=True)
        if b64_video.startswith("data:video/") and "," in b64_video:
            b64_video = b64_video.split(",", 1)[1]
        output_path.write_bytes(base64.b64decode(b64_video))
        return True

    video_url = find_video_url(payload)
    if not video_url:
        return False

    output_path.parent.mkdir(parents=True, exist_ok=True)
    resolved_url = urllib.parse.urljoin(base_url.rstrip("/") + "/", video_url)
    headers = {"Accept": "video/mp4,video/*,*/*"}
    if api_key and same_provider_url(video_url, base_url):
        headers["Authorization"] = f"Bearer {api_key}"
    request = urllib.request.Request(resolved_url, headers=headers)
    with urllib.request.urlopen(request, timeout=300) as response:
        output_path.write_bytes(response.read())
    return True


def build_image_prompt(asset: PromoAsset) -> str:
    return f"""{PROMO_STYLE_PROMPT}
Promo stage: {asset.stage}
Scene to create: {asset.prompt}

Output requirements:
- One polished photoreal image for a Promo render fixture
- Composition should leave useful center-safe space for renderer-added headline
- No generated marketing copy, captions, logos, UI labels, or watermark
"""


def build_video_prompt(
    asset: PromoAsset,
    seconds: int,
    aspect_ratio: str,
) -> str:
    return f"""{PROMO_STYLE_PROMPT}
Promo stage: {asset.stage}
Scene to create: {asset.prompt}

Output requirements:
- Exactly about {seconds} seconds
- Landscape-friendly {aspect_ratio} composition with the main action centered
- Natural camera movement and believable human motion
- Include a clearly audible, natural English spoken voiceover; speech must be
  intelligible and mixed above ambience
- Voiceover text, spoken verbatim: "{asset.voiceover}"
- No captions, no on-screen text, no logos, no watermark, and no music that
  competes with the voice
"""


def ffprobe_video(path: Path) -> dict[str, Any] | None:
    ffprobe = shutil.which("ffprobe")
    if not ffprobe:
        return None
    command = [
        ffprobe,
        "-v",
        "error",
        "-show_entries",
        "format=duration:stream=index,codec_type,width,height,codec_name",
        "-of",
        "json",
        str(path),
    ]
    result = subprocess.run(command, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"ffprobe failed for {path}: {result.stderr.strip()}")
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError(f"ffprobe returned invalid JSON for {path}") from error


def validate_video(path: Path, requested_seconds: int) -> dict[str, Any]:
    if not path.is_file() or path.stat().st_size == 0:
        raise RuntimeError(f"Generated video is missing or empty: {path}")

    probe = ffprobe_video(path)
    if probe is not None:
        streams = probe.get("streams", [])
        video_streams = [stream for stream in streams if stream.get("codec_type") == "video"]
        audio_streams = [stream for stream in streams if stream.get("codec_type") == "audio"]
        if not video_streams:
            raise RuntimeError(f"Generated video has no video stream: {path}")
        if not audio_streams:
            raise RuntimeError(f"Generated video has no audio stream: {path}")
        duration = float(probe.get("format", {}).get("duration") or 0)
        return {
            "method": "ffprobe",
            "has_video": True,
            "has_audio": True,
            "duration_seconds": round(duration, 3),
            "width": video_streams[0].get("width"),
            "height": video_streams[0].get("height"),
            "video_codec": video_streams[0].get("codec_name"),
            "audio_codec": audio_streams[0].get("codec_name"),
        }

    # MP4 track handler fallback for environments without ffprobe.
    contents = path.read_bytes()
    lower_contents = contents.lower()
    has_video = b"vide" in lower_contents
    has_audio = b"soun" in lower_contents
    if not has_video:
        raise RuntimeError(f"Generated MP4 has no detectable video track: {path}")
    if not has_audio:
        raise RuntimeError(f"Generated MP4 has no detectable audio track: {path}")
    return {
        "method": "mp4_atom_scan",
        "has_video": True,
        "has_audio": True,
        "duration_seconds": requested_seconds,
        "duration_verified": False,
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate sample FrameMio Promo photos and voiced videos."
    )
    parser.add_argument(
        "--env-file",
        type=Path,
        default=DEFAULT_ENV_FILE,
        help=f"Provider env file. Defaults to {DEFAULT_ENV_FILE}.",
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=DEFAULT_OUTPUT_DIR,
        help=f"Output directory. Defaults to {DEFAULT_OUTPUT_DIR}.",
    )
    parser.add_argument(
        "--only",
        choices=[asset.slug for asset in PROMO_ASSETS],
        help="Generate only one Promo asset.",
    )
    parser.add_argument("--dry-run", action="store_true", help="Print requests without calling providers.")
    parser.add_argument("--skip-images", action="store_true", help="Do not generate photo assets.")
    parser.add_argument("--skip-videos", action="store_true", help="Do not generate video assets.")
    parser.add_argument("--image-api-key", default="", help="Override image API key.")
    parser.add_argument("--image-base-url", default="", help="Override image base URL.")
    parser.add_argument("--image-model", default=DEFAULT_IMAGE_MODEL)
    parser.add_argument("--image-size", default=DEFAULT_IMAGE_SIZE)
    parser.add_argument("--image-quality", default=DEFAULT_IMAGE_QUALITY)
    parser.add_argument(
        "--image-format",
        default=DEFAULT_IMAGE_FORMAT,
        choices=["png", "jpeg", "webp"],
    )
    parser.add_argument("--video-api-key", default="", help="Override video API key.")
    parser.add_argument("--video-base-url", default="", help="Override video base URL.")
    parser.add_argument("--video-model", default=DEFAULT_VIDEO_MODEL)
    parser.add_argument(
        "--video-seconds",
        type=int,
        help="Override every generated video duration, from 1 to 15 seconds.",
    )
    parser.add_argument("--video-aspect-ratio", default=DEFAULT_VIDEO_ASPECT_RATIO)
    parser.add_argument(
        "--video-resolution",
        default=DEFAULT_VIDEO_RESOLUTION,
        choices=["480p", "720p", "1080p"],
    )
    parser.add_argument("--poll-interval", type=int, default=DEFAULT_POLL_INTERVAL)
    parser.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    load_env_file(args.env_file.expanduser())

    if args.video_seconds is not None and not 1 <= args.video_seconds <= 15:
        print("--video-seconds must be between 1 and 15", file=sys.stderr)
        return 2
    if args.video_model == "grok-imagine-video" and args.video_resolution == "1080p":
        print(
            "grok-imagine-video supports up to 720p; use grok-imagine-video-1.5-preview for 1080p",
            file=sys.stderr,
        )
        return 2

    selected = [asset for asset in PROMO_ASSETS if not args.only or asset.slug == args.only]
    if args.skip_images:
        selected = [asset for asset in selected if asset.kind != "photo"]
    if args.skip_videos:
        selected = [asset for asset in selected if asset.kind != "video"]
    if not selected:
        print("No assets selected.", file=sys.stderr)
        return 2

    load_env_file(args.env_file.expanduser())
    output_dir = args.output_dir.expanduser()
    image_key = (
        args.image_api_key.strip()
        or os.environ.get("FDE_IMAGE_API_KEY", "").strip()
        or os.environ.get("GATEWAY_IMAGE_UPSTREAM_API_KEY", "").strip()
        or os.environ.get("GATEWAY_UPSTREAM_API_KEY", "").strip()
        or os.environ.get("OPENAI_API_KEY", "").strip()
    )
    image_base_url = (
        args.image_base_url.strip()
        or os.environ.get("FDE_IMAGE_BASE_URL", "").strip()
        or os.environ.get("GATEWAY_IMAGE_UPSTREAM_BASE_URL", "").strip()
        or os.environ.get("GATEWAY_UPSTREAM_BASE_URL", "").strip()
        or os.environ.get("OPENAI_BASE_URL", "").strip()
        or "https://api.openai.com"
    )
    video_key = (
        args.video_api_key.strip()
        or os.environ.get("GATEWAY_VIDEO_UPSTREAM_API_KEY", "").strip()
        or os.environ.get("SEEDANCE_VIDEO_API_KEY", "").strip()
        or os.environ.get("GATEWAY_GROK_UPSTREAM_API_KEY", "").strip()
        or os.environ.get("GATEWAY_UPSTREAM_API_KEY", "").strip()
    )
    video_base_url = (
        args.video_base_url.strip()
        or os.environ.get("GATEWAY_VIDEO_UPSTREAM_BASE_URL", "").strip()
        or os.environ.get("SEEDANCE_VIDEO_BASE_URL", "").strip()
        or "https://task-api-1-cn.65535.space"
    )

    image_url = normalize_images_url(image_base_url)
    video_url = video_create_url(video_base_url)
    manifest: list[dict[str, Any]] = []

    for index, asset in enumerate(selected, start=1):
        duration_seconds = args.video_seconds or asset.duration_seconds
        validation: dict[str, Any] = {"dry_run": True}
        if asset.kind == "photo":
            extension = "jpg" if args.image_format == "jpeg" else args.image_format
            output_path = output_dir / f"{asset.slug}.{extension}"
            prompt = build_image_prompt(asset)
            payload: dict[str, Any] = {
                "model": args.image_model,
                "prompt": prompt,
                "size": args.image_size,
                "quality": args.image_quality,
                "output_format": args.image_format,
            }
            if args.dry_run:
                print(f"--- {asset.slug} photo ---")
                print(f"POST {image_url}")
                print(json.dumps(payload, indent=2, ensure_ascii=False))
            else:
                if not image_key:
                    print(
                        "Missing image API key. Set FDE_IMAGE_API_KEY, "
                        "GATEWAY_IMAGE_UPSTREAM_API_KEY, or OPENAI_API_KEY.",
                        file=sys.stderr,
                    )
                    return 2
                print(f"[{index}/{len(selected)}] Generating photo {asset.slug}")
                result = request_json(image_url, image_key, payload, method="POST")
                save_image(result, output_path, image_base_url, image_key)
                if output_path.stat().st_size == 0:
                    raise RuntimeError(f"Generated image is empty: {output_path}")
                print(f"Saved {output_path}")
            if not args.dry_run:
                validation = {"non_empty_file": True}
        else:
            output_path = output_dir / f"{asset.slug}.mp4"
            prompt = build_video_prompt(asset, duration_seconds, args.video_aspect_ratio)
            payload = {
                "model": args.video_model,
                "prompt": prompt,
                "seconds": duration_seconds,
                "resolution": args.video_resolution,
                "aspect_ratio": args.video_aspect_ratio,
                "images": [],
            }
            if args.dry_run:
                print(f"--- {asset.slug} video ---")
                print(f"POST {video_url}")
                print(json.dumps(payload, indent=2, ensure_ascii=False))
            else:
                if not video_key:
                    print(
                        "Missing video API key. Set GATEWAY_VIDEO_UPSTREAM_API_KEY, "
                        "SEEDANCE_VIDEO_API_KEY, or GATEWAY_GROK_UPSTREAM_API_KEY.",
                        file=sys.stderr,
                    )
                    return 2
                print(
                    f"[{index}/{len(selected)}] Generating {duration_seconds}s video {asset.slug}"
                )
                create_payload = request_json(video_url, video_key, payload, method="POST")
                if not save_video_from_payload(
                    create_payload,
                    output_path,
                    video_base_url,
                    video_key,
                ):
                    video_id = find_video_id(create_payload)
                    if not video_id:
                        print(
                            json.dumps(create_payload, indent=2, ensure_ascii=False)[:2000],
                            file=sys.stderr,
                        )
                        raise RuntimeError(
                            "Video response did not include a video URL or task id"
                        )
                    print(
                        f"Created video task {video_id} "
                        f"status={payload_status(create_payload) or 'unknown'}"
                    )
                    deadline = time.monotonic() + args.timeout
                    latest_payload = create_payload
                    while time.monotonic() < deadline:
                        time.sleep(max(1, args.poll_interval))
                        latest_payload = request_json(
                            video_status_url(video_base_url, video_id),
                            video_key,
                        )
                        status = payload_status(latest_payload)
                        progress = payload_progress(latest_payload)
                        print(
                            f"Status {video_id}: {status or 'unknown'} "
                            f"{progress + '%' if progress else ''}".rstrip()
                        )
                        if save_video_from_payload(
                            latest_payload,
                            output_path,
                            video_base_url,
                            video_key,
                        ):
                            break
                        if status in TERMINAL_STATUSES:
                            break
                    if not output_path.is_file():
                        if payload_status(latest_payload) not in SUCCESS_STATUSES:
                            print(
                                json.dumps(
                                    latest_payload,
                                    indent=2,
                                    ensure_ascii=False,
                                )[:2000],
                                file=sys.stderr,
                            )
                            raise RuntimeError(
                                "Video task did not complete successfully: "
                                f"{payload_status(latest_payload) or 'unknown'}"
                            )
                        raise RuntimeError(
                            "Video task completed but no downloadable URL was found"
                        )
                validation = validate_video(output_path, duration_seconds)
                print(f"Saved {output_path} ({json.dumps(validation, sort_keys=True)})")

        manifest.append(
            {
                "project_type": "promo",
                "slug": asset.slug,
                "title": asset.title,
                "stage": asset.stage,
                "kind": asset.kind,
                "path": str(output_path),
                "duration_seconds": duration_seconds,
                "voiceover": asset.voiceover or None,
                "aspect_ratio": args.video_aspect_ratio if asset.kind == "video" else None,
                "prompt": prompt,
                "validation": validation,
            }
        )
        if not args.dry_run and index < len(selected):
            time.sleep(1)

    if not args.dry_run:
        output_dir.mkdir(parents=True, exist_ok=True)
        manifest_path = output_dir / "manifest.json"
        manifest_path.write_text(
            json.dumps(manifest, indent=2, ensure_ascii=False),
            encoding="utf-8",
        )
        print(f"Saved manifest to {manifest_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
