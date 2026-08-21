# Transcription Service

## Purpose

FrameMio uses a provider-independent transcription service inside the Video Intelligence Worker. The service accepts an S3-backed audio or video reference and returns a normalized, timestamped transcript without exposing a specific transcription vendor to the rest of the application.

It does not generate StoryPlans and does not render video.

## Boundary

```text
Video Intelligence Worker
    -> extract or identify audio/video object in S3
    -> Transcription Service
        -> transcript cache lookup in PostgreSQL
        -> short-lived S3 reference
        -> TranscriptionProvider
        -> timestamp validation and normalization
        -> optional raw provider artifact in S3
        -> completed transcript metadata in PostgreSQL
    -> VideoEvidence
```

The `TranscriptionProvider` interface is the only vendor-specific boundary. Provider adapters receive a reference with media type and duration and return canonical timestamped segments plus optional raw JSON.

## Canonical Transcript

Internal timestamps are integer microseconds to avoid floating-point drift across FFmpeg frames, transcript segments, semantic moments, and later story processing. HTTP providers may return seconds such as `3.2`; adapters convert those values before the service validates them.

A completed transcript records:

- user, project, and media identifiers;
- source S3 ETag and transcription version;
- provider and provider model;
- language and whether speech was detected;
- media duration and segment count;
- normalized transcript text and timestamped segments;
- optional raw provider artifact S3 key;
- creation and update timestamps.

Empty segments are valid and represent media with no detected speech. Low-confidence segments are retained for noisy audio rather than silently discarded.

## Cache and Retries

The logical transcript cache key is:

```text
media ID + transcription version + source S3 ETag
```

Only validated, completed transcripts are persisted. Duplicate requests and worker retries reuse the existing PostgreSQL record without calling the provider or generating another signed URL. Concurrent saves use the existing unique logical-key constraint and return the winning record.

Provider calls and large raw-artifact uploads use bounded exponential retries and honor context cancellation. Long audio is sent by reference, not loaded into a JSON request, and uses a dedicated provider timeout.

## Storage

PostgreSQL stores canonical transcript records and metadata in `framemio_domain_records` with kind `transcript`. Large raw provider responses use the deterministic S3 path:

```text
users/{userID}/transcriptions/{mediaID}/{version}/{etagHash}/provider-response.json
```

The raw-artifact threshold is configurable. Provider response bytes, media bytes, and transcripts are never added to SQS messages.

## Failure Behavior

- Invalid request identifiers, duration, source key, or media type fail permanently.
- Transient provider and artifact-storage failures are retried.
- Invalid timestamp ranges, empty segment text, and invalid confidence values are rejected.
- A final provider failure returns an error and does not create a cached transcript.
- Cancellation stops retry waits and propagates to the provider HTTP request.

## Configuration

- `TRANSCRIPTION_API_URL`
- `TRANSCRIPTION_API_KEY`
- `TRANSCRIPTION_PROVIDER`
- `TRANSCRIPTION_MODEL`
- `TRANSCRIPTION_VERSION`
- `ENABLE_TRANSCRIPTION`
- `TRANSCRIPTION_RETRIES`
- `TRANSCRIPTION_PROVIDER_TIMEOUT`
- `TRANSCRIPTION_RAW_ARTIFACT_THRESHOLD_BYTES`
- `EVIDENCE_URL_EXPIRY`

Legacy `SPEECH_*` variables remain fallback aliases for existing environments.
