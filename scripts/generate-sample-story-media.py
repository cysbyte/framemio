#!/usr/bin/env python3
"""Generate FrameMio sample story media using GPT Image and Grok-compatible video APIs."""

from __future__ import annotations

import argparse
import base64
import json
import os
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
DEFAULT_OUTPUT_DIR = None
DEFAULT_SCENARIO = "family-day"
DEFAULT_IMAGE_MODEL = "gpt-image-2"
DEFAULT_IMAGE_SIZE = "1024x1024"
DEFAULT_IMAGE_QUALITY = "high"
DEFAULT_IMAGE_FORMAT = "png"
DEFAULT_VIDEO_MODEL = "grok-imagine-video"
DEFAULT_VIDEO_SECONDS = 5
DEFAULT_VIDEO_ASPECT_RATIO = "3:4"
DEFAULT_VIDEO_RESOLUTION = "720p"
DEFAULT_POLL_INTERVAL = 5
DEFAULT_TIMEOUT = 900

Kind = Literal["photo", "video"]

STYLE_PROMPT = """Create synthetic photoreal media for FrameMio, an AI memory filmmaker for iPhone users.
The output should look like it was casually captured on a recent iPhone during one warm family day, not like a stock photo, ad, movie still, illustration, or app screenshot.
Use natural handheld composition, believable iPhone color science, slight depth-of-field, gentle motion blur where appropriate, warm late-morning-to-golden-hour light, and a calm premium Apple Photos memory feeling.
Keep all people synthetic and non-identifiable: no direct eye contact, no celebrity likeness, no recognizable real person, faces turned away, cropped, side/back view, or softly out of focus.
No text, no captions, no logos, no UI, no watermarks, no trademarks, no readable signs, no distorted hands or extra limbs.
Maintain continuity across the set: same small family, same casual summer clothing palette, breakfast at home, walk through a park, lake, sunset, and laughing together near the water.
"""


@dataclass(frozen=True)
class SampleMoment:
    slug: str
    title: str
    kind: Kind
    prompt: str


@dataclass(frozen=True)
class SampleScenario:
    slug: str
    title: str
    continuity: str
    style_prompt: str
    moments: list[SampleMoment]


FAMILY_DAY_STYLE_PROMPT = STYLE_PROMPT
FAMILY_DAY_MOMENTS: list[SampleMoment] = [
    SampleMoment(
        slug="01-breakfast",
        title="Breakfast",
        kind="photo",
        prompt="""A candid iPhone photo at 8:32 AM: family breakfast at a sunlit kitchen table, toast, fruit, mugs, small hands reaching for food, warm window light, cozy lived-in home, people cropped at shoulders or softly out of focus, authentic personal memory snapshot.""",
    ),
    SampleMoment(
        slug="02-walking",
        title="Walking",
        kind="photo",
        prompt="""A candid iPhone photo at 10:15 AM: the same family walking together through a green park path, seen from behind, soft morning light through trees, relaxed movement, stroller or tote bag detail, natural phone framing with slight motion blur.""",
    ),
    SampleMoment(
        slug="03-lake",
        title="Lake",
        kind="photo",
        prompt="""A candid iPhone photo at 11:20 AM: the same family near a quiet lake, one person pointing toward the water, picnic blanket edge in foreground, soft reflections, relaxed summer day, faces not identifiable, warm natural color.""",
    ),
    SampleMoment(
        slug="04-sunset",
        title="Sunset",
        kind="video",
        prompt="""A 5-second handheld iPhone video clip at golden hour near the same lake: warm sunset reflections on water, the family silhouettes walking slowly along the shore, gentle camera sway, natural autofocus breathing, quiet cinematic memory feeling, no cuts that feel like stock footage.""",
    ),
    SampleMoment(
        slug="05-laughing",
        title="Laughing",
        kind="video",
        prompt="""A 5-second handheld iPhone video clip near the lake: the same family laughing together on a picnic blanket, a playful candid moment, camera starts on hands and blanket then tilts gently toward people from behind/side, faces non-identifiable, emotionally warm and personal.""",
    ),
]

FAMILY_TALK_STYLE_PROMPT = """Create synthetic photoreal media for FrameMio, an AI memory filmmaker for iPhone users.
The output should look like it was casually captured on a recent iPhone during one warm family picnic day, not like a stock photo, ad, movie still, illustration, or app screenshot.
Use natural handheld composition, believable iPhone color science, slight depth-of-field, warm indoor-to-golden-hour light, and a calm Apple Photos memory feeling.
Keep all people synthetic and non-identifiable: no direct eye contact, no celebrity likeness, no recognizable real person, faces turned away, cropped, side/back view, or softly out of focus.
For video moments, include clear natural ambient audio and short spoken English dialogue suitable for transcription testing. The speech should sound like casual family conversation recorded by an iPhone microphone, with no music overpowering the voices.
No text, no captions, no logos, no UI, no watermarks, no trademarks, no readable signs, no distorted hands or extra limbs.
Maintain continuity across the set: same small synthetic family, same warm clothing palette, breakfast planning, park walk, lake picnic, sunset chat, and laughing conversation.
"""

FAMILY_TALK_MOMENTS: list[SampleMoment] = [
    SampleMoment(
        slug="01-breakfast-planning",
        title="Breakfast Planning",
        kind="photo",
        prompt="""A candid iPhone photo around a kitchen table after breakfast: picnic bag, fruit container, coffee mugs, a folded blanket, and family hands getting ready for a lake day; warm window light, faces cropped or softly out of focus, authentic camera-roll feel.""",
    ),
    SampleMoment(
        slug="02-kitchen-talk",
        title="Kitchen Talk",
        kind="video",
        prompt="""An 8-second handheld iPhone video in the same kitchen after breakfast. Clear natural spoken English dialogue for transcription: one adult says, "Should we go to the lake after breakfast?" another voice replies, "Yes, let's bring the picnic blanket." Include quiet kitchen ambience, gentle hand movement packing fruit, non-identifiable people, no captions or text.""",
    ),
    SampleMoment(
        slug="03-park-walk",
        title="Park Walk",
        kind="photo",
        prompt="""A candid iPhone photo late morning: the same family walking through a green park toward the lake, seen from behind, tote bag and picnic blanket visible, soft light through trees, natural phone framing, no readable signs.""",
    ),
    SampleMoment(
        slug="04-lake-ducks-talk",
        title="Lake Ducks Talk",
        kind="video",
        prompt="""An 8-second handheld iPhone video near a quiet lake. Clear natural spoken English dialogue for transcription: a childlike voice says, "Look at the ducks by the water," and an adult laughs softly and says, "This is the best little picnic." Keep faces turned away or blurred, include lake ambience and footsteps, no music, no captions.""",
    ),
    SampleMoment(
        slug="05-sunset-laughing-talk",
        title="Sunset Laughing Talk",
        kind="video",
        prompt="""An 8-second handheld iPhone video at sunset on a picnic blanket by the lake. Clear natural spoken English dialogue for transcription: one person says, "Remember when the strawberries spilled?" another replies while laughing, "That was the funniest part of today." Warm golden light, casual laughter, non-identifiable people, no captions, no text.""",
    ),
]

SCENARIOS: dict[str, SampleScenario] = {
    "family-day": SampleScenario(
        slug="family-day",
        title="Family Day",
        continuity="Breakfast → Walking → Lake → Sunset → Laughing",
        style_prompt=FAMILY_DAY_STYLE_PROMPT,
        moments=FAMILY_DAY_MOMENTS,
    ),
    "family-talk": SampleScenario(
        slug="family-talk",
        title="Family Talk",
        continuity="Breakfast planning → Kitchen talk → Park walk → Lake ducks talk → Sunset laughing talk",
        style_prompt=FAMILY_TALK_STYLE_PROMPT,
        moments=FAMILY_TALK_MOMENTS,
    ),
}

TERMINAL_STATUSES = {"completed", "succeeded", "success", "failed", "cancelled", "canceled", "error"}
SUCCESS_STATUSES = {"completed", "succeeded", "success"}


def load_env_file(path: Path) -> None:
    if not path.is_file():
        return
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        if line.startswith("export "):
            line = line[len("export "):].strip()
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


def request_json(url: str, api_key: str, payload: dict[str, Any] | None = None, method: str = "GET") -> dict[str, Any]:
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
    return find_string(payload, {"video_url", "download_url", "url", "output_url", "file_url", "content_url"})


def payload_status(payload: dict[str, Any]) -> str:
    value = payload.get("status")
    return str(value).strip().lower() if value is not None else ""


def payload_progress(payload: dict[str, Any]) -> str:
    value = payload.get("progress")
    return "" if value is None else str(value)


def save_image(result: dict[str, Any], output_path: Path) -> None:
    data = result.get("data")
    if not isinstance(data, list) or not data:
        raise RuntimeError(f"No image data returned: {json.dumps(result, ensure_ascii=False)[:1000]}")
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
        with urllib.request.urlopen(image_url, timeout=300) as response:
            output_path.write_bytes(response.read())
        return
    raise RuntimeError(f"Image response has neither b64_json nor url: {json.dumps(first, ensure_ascii=False)[:1000]}")


def save_video_from_payload(payload: dict[str, Any], output_path: Path, base_url: str = "", api_key: str = "") -> bool:
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
    resolved_url = urllib.parse.urljoin(base_url.rstrip("/") + "/", video_url) if base_url else video_url
    headers = {"Accept": "video/mp4,video/*,*/*"}
    if api_key and base_url and (video_url.startswith("/") or urllib.parse.urlparse(resolved_url).netloc == urllib.parse.urlparse(base_url).netloc):
        headers["Authorization"] = f"Bearer {api_key}"
    request = urllib.request.Request(resolved_url, headers=headers)
    with urllib.request.urlopen(request, timeout=300) as response:
        output_path.write_bytes(response.read())
    return True


def build_image_prompt(moment: SampleMoment, scenario: SampleScenario) -> str:
    return f"""{scenario.style_prompt}
Scene to create: {moment.prompt}

Output requirements:
- Single photoreal image that looks like a candid iPhone photo from a personal camera roll
- Square composition, useful for app thumbnails and easy crop testing
- Moment label for internal planning only: {moment.title}
- Keep visual continuity with the other moments: {scenario.continuity}
"""


def build_video_prompt(moment: SampleMoment, scenario: SampleScenario, seconds: int) -> str:
    return f"""{scenario.style_prompt}
Scene to create: {moment.prompt}

Output requirements:
- {seconds}-second photoreal video that looks recorded on an iPhone, not generated stock footage
- Portrait-friendly {DEFAULT_VIDEO_ASPECT_RATIO} composition with important action centered
- Natural handheld phone movement, subtle autofocus/autoexposure feel, realistic human motion
- Moment label for internal planning only: {moment.title}
- Keep visual continuity with the other moments: {scenario.continuity}
"""


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate sample FrameMio family-day photos/videos for create-story testing.")
    parser.add_argument("--env-file", type=Path, default=DEFAULT_ENV_FILE, help=f"Provider env file. Defaults to {DEFAULT_ENV_FILE}.")
    parser.add_argument("--scenario", choices=sorted(SCENARIOS), default=DEFAULT_SCENARIO, help=f"Sample scenario. Defaults to {DEFAULT_SCENARIO}.")
    parser.add_argument("--output-dir", type=Path, default=DEFAULT_OUTPUT_DIR, help="Output directory. Defaults to sample-media/<scenario>.")
    parser.add_argument("--only", help="Generate only one sample moment slug from the chosen scenario.")
    parser.add_argument("--dry-run", action="store_true", help="Print requests without calling providers.")
    parser.add_argument("--skip-images", action="store_true", help="Do not generate photo samples.")
    parser.add_argument("--skip-videos", action="store_true", help="Do not generate video samples.")
    parser.add_argument("--image-api-key", default="", help="Override image API key.")
    parser.add_argument("--image-base-url", default="", help="Override image base URL.")
    parser.add_argument("--image-model", default=DEFAULT_IMAGE_MODEL, help=f"Image model. Defaults to {DEFAULT_IMAGE_MODEL}.")
    parser.add_argument("--image-size", default=DEFAULT_IMAGE_SIZE, help=f"Image size. Defaults to {DEFAULT_IMAGE_SIZE}.")
    parser.add_argument("--image-quality", default=DEFAULT_IMAGE_QUALITY, help="Image quality: auto, low, medium, or high.")
    parser.add_argument("--image-format", default=DEFAULT_IMAGE_FORMAT, choices=["png", "jpeg", "webp"], help="Image output format.")
    parser.add_argument("--video-api-key", default="", help="Override video API key.")
    parser.add_argument("--video-base-url", default="", help="Override video base URL.")
    parser.add_argument("--video-model", default=DEFAULT_VIDEO_MODEL, help=f"Video model. Defaults to {DEFAULT_VIDEO_MODEL}.")
    parser.add_argument("--video-seconds", type=int, default=DEFAULT_VIDEO_SECONDS, help="Video duration, 1-15 seconds.")
    parser.add_argument("--video-aspect-ratio", default=DEFAULT_VIDEO_ASPECT_RATIO, help=f"Video aspect ratio. Defaults to {DEFAULT_VIDEO_ASPECT_RATIO}.")
    parser.add_argument("--video-resolution", default=DEFAULT_VIDEO_RESOLUTION, choices=["480p", "720p", "1080p"], help="Video resolution.")
    parser.add_argument("--poll-interval", type=int, default=DEFAULT_POLL_INTERVAL, help="Seconds between video status polls.")
    parser.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT, help="Maximum seconds to wait for each video.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    load_env_file(args.env_file.expanduser())

    if args.video_seconds < 1 or args.video_seconds > 15:
        print("--video-seconds must be between 1 and 15", file=sys.stderr)
        return 2
    if args.video_model == "grok-imagine-video" and args.video_resolution == "1080p":
        print("grok-imagine-video supports up to 720p; use grok-imagine-video-1.5-preview for 1080p", file=sys.stderr)
        return 2

    scenario = SCENARIOS[args.scenario]
    valid_slugs = {moment.slug for moment in scenario.moments}
    if args.only and args.only not in valid_slugs:
        print(f"--only must be one of: {', '.join(sorted(valid_slugs))}", file=sys.stderr)
        return 2

    selected = [moment for moment in scenario.moments if not args.only or moment.slug == args.only]
    if args.skip_images:
        selected = [moment for moment in selected if moment.kind != "photo"]
    if args.skip_videos:
        selected = [moment for moment in selected if moment.kind != "video"]

    output_dir = (args.output_dir or (REPO_ROOT / "sample-media" / scenario.slug)).expanduser()
    manifest: list[dict[str, Any]] = []

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
        or "https://api.cheaperinference.com/v1"
    )

    image_url = normalize_images_url(image_base_url)
    video_url = video_create_url(video_base_url)

    for index, moment in enumerate(selected, start=1):
        extension = args.image_format if args.image_format != "jpeg" else "jpg"
        output_path = output_dir / (f"{moment.slug}.{extension}" if moment.kind == "photo" else f"{moment.slug}.mp4")
        if moment.kind == "photo":
            prompt = build_image_prompt(moment, scenario)
            payload = {
                "model": args.image_model,
                "prompt": prompt,
                "size": args.image_size,
                "quality": args.image_quality,
                "output_format": args.image_format,
            }
            if args.dry_run:
                print(f"--- {moment.slug} photo ---")
                print(f"POST {image_url}")
                print(json.dumps(payload, indent=2, ensure_ascii=False))
            else:
                if not image_key:
                    print("Missing image API key. Set FDE_IMAGE_API_KEY, GATEWAY_IMAGE_UPSTREAM_API_KEY, or OPENAI_API_KEY.", file=sys.stderr)
                    return 2
                print(f"[{index}/{len(selected)}] Generating photo {moment.slug} with {args.image_model}")
                result = request_json(image_url, image_key, payload, method="POST")
                save_image(result, output_path)
                print(f"Saved {output_path}")
        else:
            prompt = build_video_prompt(moment, scenario, args.video_seconds)
            payload = {
                "model": args.video_model,
                "prompt": prompt,
                "seconds": args.video_seconds,
                "resolution": args.video_resolution,
                "aspect_ratio": args.video_aspect_ratio,
                "images": [],
            }
            if args.dry_run:
                print(f"--- {moment.slug} video ---")
                print(f"POST {video_url}")
                print(json.dumps(payload, indent=2, ensure_ascii=False))
            else:
                if not video_key:
                    print("Missing video API key. Set GATEWAY_VIDEO_UPSTREAM_API_KEY, SEEDANCE_VIDEO_API_KEY, or GATEWAY_GROK_UPSTREAM_API_KEY.", file=sys.stderr)
                    return 2
                print(f"[{index}/{len(selected)}] Generating video {moment.slug} with {args.video_model}")
                create_payload = request_json(video_url, video_key, payload, method="POST")
                if not save_video_from_payload(create_payload, output_path, video_base_url, video_key):
                    video_id = find_video_id(create_payload)
                    if not video_id:
                        print(json.dumps(create_payload, indent=2, ensure_ascii=False)[:2000], file=sys.stderr)
                        raise RuntimeError("Video response did not include a video URL or task id")
                    print(f"Created video task {video_id} status={payload_status(create_payload) or 'unknown'}")
                    deadline = time.monotonic() + args.timeout
                    latest_payload = create_payload
                    while time.monotonic() < deadline:
                        time.sleep(max(1, args.poll_interval))
                        latest_payload = request_json(video_status_url(video_base_url, video_id), video_key)
                        status = payload_status(latest_payload)
                        progress = payload_progress(latest_payload)
                        print(f"Status {video_id}: {status or 'unknown'} {progress + '%' if progress else ''}".rstrip())
                        if save_video_from_payload(latest_payload, output_path, video_base_url, video_key):
                            break
                        if status in TERMINAL_STATUSES:
                            break
                    if not output_path.is_file():
                        if payload_status(latest_payload) not in SUCCESS_STATUSES:
                            print(json.dumps(latest_payload, indent=2, ensure_ascii=False)[:2000], file=sys.stderr)
                            raise RuntimeError(f"Video task did not complete successfully: {payload_status(latest_payload) or 'unknown'}")
                        raise RuntimeError("Video task completed but no downloadable URL was found")
                print(f"Saved {output_path}")

        manifest.append({
            "slug": moment.slug,
            "title": moment.title,
            "kind": moment.kind,
            "path": str(output_path),
            "prompt": build_image_prompt(moment, scenario) if moment.kind == "photo" else build_video_prompt(moment, scenario, args.video_seconds),
        })
        if not args.dry_run and index < len(selected):
            time.sleep(1)

    if not args.dry_run:
        output_dir.mkdir(parents=True, exist_ok=True)
        manifest_path = output_dir / "manifest.json"
        manifest_path.write_text(json.dumps(manifest, indent=2, ensure_ascii=False), encoding="utf-8")
        print(f"Saved manifest to {manifest_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
