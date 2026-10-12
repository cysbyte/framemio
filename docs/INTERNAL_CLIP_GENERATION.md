# Service clip generation

FrameMio exposes an opt-in, user-login-free API for the `product-video` service. It is **not public or unauthenticated**: every request needs `X-FrameMio-Clip-Token`, set to a dedicated secret in `CLIP_GENERATION_TOKEN` (at least 32 characters and different from `INTERNAL_API_TOKEN`). Leave the variable unset to disable these routes. Pass the secret only from a server, over HTTPS; never put it in a browser bundle or a query string. Set the GitHub Actions secret `CLIP_GENERATION_TOKEN` or provide `CLIP_GENERATION_TOKEN_SECRET_ARN` for the backend ECS deployment.

Requests run as the fixed `service-product-video` account; they cannot access another user's photos or generations. Existing credit reservation, commit, and failure refund behavior applies. This service account receives its one-time initial grant; replenish its credits deliberately through your normal operational credit process. New requests have an in-memory limit of two per minute and twenty per day **per API process**. This is a guardrail, not a distributed spending limit: configure provider-side spend limits when available.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/internal/clip-videos` | Create a video; requires `Idempotency-Key`. |
| `GET` | `/internal/clip-videos/{id}` | Poll status and retrieve metadata including `s3_key`. |
| `GET` | `/internal/clip-videos/{id}/content` | Redirect to an expiring signed S3 download URL after completion. |
| `POST` | `/internal/clip-media/upload-session` | Authorize a photo upload for image-to-video; requires `Idempotency-Key`. |
| `POST` | `/internal/clip-media/complete` | Mark a successfully uploaded photo ready. |

Create a text-to-video clip with JSON such as `{"prompt":"A support agent pauses while switching between systems","seconds":6,"resolution":"720p","aspect_ratio":"16:9"}`. The response includes a stable generation `id` and `status`; poll until `completed` or `failed`. Repeating the same idempotency key returns the same generation without creating another charge. Duration is 1–15 seconds. For image-to-video, first upload a JPEG, PNG, or WebP photo (up to 10 MB) through the two clip-media endpoints, then provide its returned `media.id` as `image_media_id` in the video request. The upload-session response includes a presigned `session.parts[0].upload_url`; upload the image bytes there before calling `/complete` with `media_id`, `upload_id`, `s3_key`, and `media_type: "photo"`. Only service-owned, ready photos are accepted.

FrameMio persists successful clips at `users/service-product-video/text-videos/{id}.mp4` in its configured `S3_MEDIA_BUCKET`. Keep the returned generation ID, S3 bucket/key, prompt, content description, and any approval metadata in the `product-video` catalog. Do **not** store the signed `/content` redirect as a permanent URL: it expires after two hours. `product-video` may copy an approved clip into its own bucket for independent retention. This endpoint only generates clips; it does not add them to a course render or create the catalog.
