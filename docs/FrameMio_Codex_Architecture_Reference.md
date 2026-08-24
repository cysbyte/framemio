# FrameMio — Architecture & Engineering Reference for Codex

## 0. Purpose of this document

This document is the architectural source of truth for FrameMio.

The project has already been implemented from a series of prompts and the AWS infrastructure has already been provisioned. The current phase is **integration testing, debugging, stabilization, and correctness fixes**.

Codex must use this document as a guardrail when inspecting the repository, diagnosing failures, and making revisions.

### Primary rule

**Fix implementation problems without changing the intended architecture unless a change is demonstrably necessary and is explicitly justified.**

Do not redesign the system merely because a local implementation is inconvenient.

Before making a significant architectural change:

1. Inspect the existing repository.
2. Identify the actual failure.
3. Determine which layer owns the failure.
4. Fix the smallest responsible layer.
5. Preserve all existing valid contracts.
6. Run the relevant tests.
7. Only propose architectural changes if the current design cannot satisfy a requirement.

---

# 1. Product definition

## 1.1 Product

**FrameMio** is an iPhone-first AI memory-video application.

The product is NOT primarily:

- a general-purpose video editor
- a CapCut clone
- an AI avatar application
- an AI image generator
- a template marketplace
- a social network

The core product idea is:

> **Turn everyday iPhone photos and videos into a coherent personal memory story.**

The primary experience is:

```text
Open FrameMio
    ↓
Make My Day
    ↓
FrameMio understands the selected/relevant memories
    ↓
Finds meaningful moments
    ↓
Creates a short personal film
    ↓
User watches / lightly edits / shares
```

The product should feel like an **AI filmmaker for everyday life**, not a traditional editing timeline.

---

# 2. Core architectural philosophy

The system has strict responsibility boundaries.

```text
iPhone
  = capture / select / lightweight local intelligence / upload / playback

Go API
  = authentication / authorization / metadata / orchestration / job creation

S3
  = original media / intermediate artifacts / final videos

SQS
  = asynchronous job transport

Video Intelligence Worker
  = FFmpeg / FFprobe / scene detection / frame selection / preprocessing

Transcription
  = speech → timestamped transcript

Multimodal AI
  = semantic understanding of video evidence

VideoAnalysis
  = persistent structured representation of what was understood

Story Planner
  = decide which meaningful moments form a coherent story

StoryPlan Validator
  = deterministic validation

Render Worker
  = Remotion + FFmpeg, deterministic visual composition

PostgreSQL
  = durable application state and structured analysis/job metadata
```

## 2.1 Critical separation

Do NOT collapse these concepts:

```text
VideoAnalysis
    ≠
StoryPlan
    ≠
RenderedVideo
```

They are separate artifacts.

### VideoAnalysis

Answers:

> What is happening in the media?

### StoryPlan

Answers:

> Which moments should be used, and in what order?

### Render

Answers:

> How should the selected story be visually composed into an MP4?

This separation is one of the most important architectural invariants.

---

# 3. Target system architecture

The intended production architecture is:

```text
                         ┌─────────────────────┐
                         │       iPhone        │
                         │      FrameMio       │
                         └──────────┬──────────┘
                                    │
                             HTTPS API calls
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │        ALB          │
                         │    HTTPS / TLS      │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │    ECS Fargate      │
                         │       Go API        │
                         └───────┬───────┬─────┘
                                 │       │
                    ┌────────────┘       └───────────────┐
                    ▼                                    ▼
             PostgreSQL                                  SQS
                    │                         ┌───────────┴──────────┐
                    │                         ▼                      ▼
                    │               Video Intelligence         Render Worker
                    │                    Worker                  │
                    │                         │                   │
                    │                         ▼                   ▼
                    │                  FFmpeg/FFprobe          Remotion
                    │                         │                 + FFmpeg
                    │                         ▼                   │
                    │                  Transcription             │
                    │                         │                   │
                    │                         ▼                   │
                    │                 Multimodal AI               │
                    │                         │                   │
                    │                         ▼                   │
                    │                  VideoAnalysis              │
                    │                         │                   │
                    │                         ▼                   │
                    │                   Story Planner              │
                    │                         │                   │
                    │                         ▼                   │
                    │                    StoryPlan                 │
                    │                         │                   │
                    │                         ▼                   │
                    │                  StoryPlan Validator         │
                    │                         │                   │
                    └─────────────────────────┼───────────────────┘
                                              │
                                              ▼
                                             S3
                                              │
                                              ▼
                                           iPhone
```

---

# 4. Public vs private services

## Public

Only the Go API should be publicly reachable through the ALB.

Example:

```text
https://api.framemio.com
```

The iOS app talks to the Go API.

## Private

These should NOT require public HTTP endpoints:

- Video Intelligence Worker
- Render Worker
- SQS
- PostgreSQL
- private S3 objects

Workers consume jobs asynchronously.

Do not introduce an ALB for workers just to make them reachable.

---

# 5. iOS architecture

## 5.1 iPhone responsibilities

The iPhone is responsible for:

- PhotoKit media access
- user media selection
- lightweight local media intelligence
- upload orchestration
- authentication token storage
- project/status UI
- playback
- sharing
- lightweight user corrections

The iPhone is NOT responsible for:

- full video semantic understanding
- expensive scene analysis
- large-scale video frame extraction
- multimodal AI calls for video understanding
- server-side rendering
- FFmpeg-based backend processing

## 5.2 Local media intelligence

Use native frameworks where appropriate:

- PhotoKit
- Vision
- Core Image
- AVFoundation

For photos, lightweight analysis may include:

- asset ID
- timestamp
- location if available and permitted
- dimensions
- orientation
- file type
- blur/quality indicators
- brightness/exposure
- face information
- people count
- basic Vision classifications
- duplicate/near-duplicate information

For videos, collect lightweight metadata:

- asset ID
- timestamp
- location if available and permitted
- duration
- dimensions
- orientation
- FPS
- file size
- audio availability
- basic quality indicators

Do not turn the iPhone into a full video-analysis engine.

---

# 6. Media upload architecture

Large media must NOT pass through the Go API.

Correct:

```text
iPhone
   ↓
POST /media/upload-session
   ↓
Go API
   ↓
presigned S3 URL(s)
   ↓
iPhone
   ↓
direct upload
   ↓
S3
   ↓
notify Go API
```

For large videos:

- use multipart upload
- support retries
- support resumability where practical
- do not restart successfully uploaded parts
- do not store video bytes in PostgreSQL
- do not put video bytes into SQS

The backend stores references such as:

```text
mediaId
s3Key
mediaType
duration
size
checksum where available
```

---

# 7. Authentication and authorization

## 7.1 Product requirement

FrameMio should not force account creation before the user can use the core experience.

The intended UX is:

```text
Install
  ↓
Open
  ↓
Make My Day
```

No mandatory sign-up wall.

## 7.2 Backend security

No hardcoded secret API key should be used as the primary security mechanism.

The app should have an authenticated application identity, initially supporting an anonymous identity.

Conceptually:

```text
First launch
   ↓
anonymous identity
   ↓
backend authentication
   ↓
access token
   ↓
API requests
```

The access token is stored securely on iOS.

## 7.3 Authorization

Every resource must be owned by an authenticated identity.

Never trust a client-supplied `userId`.

Bad:

```text
POST /projects
{
  "userId": "123"
}
```

and then trusting that value.

Correct:

```text
Authorization: Bearer <token>
             ↓
authenticated user ID
             ↓
database ownership check
```

For every project/media/job request, enforce ownership.

Example:

```sql
SELECT *
FROM projects
WHERE id = $1
AND user_id = $authenticatedUserID;
```

## 7.4 Sign in with Apple

Sign in with Apple can later upgrade/link the anonymous identity.

Do not create duplicate identities unnecessarily.

Conceptually:

```text
anonymous user
      ↓
Sign in with Apple
      ↓
link/upgrade identity
      ↓
persistent account
```

---

# 8. API architecture

The Go API is an orchestration layer.

It should NOT perform expensive video work synchronously.

Good:

```text
POST /projects/:id/analyze
        ↓
create job
        ↓
publish SQS message
        ↓
return job ID
```

Bad:

```text
POST /projects/:id/analyze
        ↓
download video
        ↓
FFmpeg
        ↓
AI
        ↓
wait 10 minutes
        ↓
return response
```

## 8.1 Example API groups

Authentication:

```text
POST /v1/auth/anonymous
POST /v1/auth/apple
POST /v1/auth/link-apple
POST /v1/auth/logout
```

Media:

```text
POST /v1/media/upload-session
POST /v1/media
GET  /v1/media/:id
```

Projects:

```text
POST /v1/projects
GET  /v1/projects
GET  /v1/projects/:id
```

Processing:

```text
POST /v1/projects/:id/analyze
GET  /v1/projects/:id/status
POST /v1/projects/:id/render
GET  /v1/projects/:id/video
```

Exact routes may differ if the repository already established a coherent convention. Do not rename routes solely to match this document.

---

# 9. SQS rules

SQS carries job references, not large data.

Good:

```json
{
  "jobId": "...",
  "projectId": "...",
  "mediaId": "..."
}
```

Bad:

```json
{
  "videoBytes": "...",
  "largeTranscript": "...",
  "thousandsOfFrames": "..."
}
```

Large data belongs in:

- S3
- PostgreSQL, when it is structured and reasonably sized

SQS should contain enough information for a worker to locate the required data.

---

# 10. Job model

Supported conceptual stages:

```text
MEDIA_PREPROCESS
TRANSCRIBE
VIDEO_UNDERSTAND
STORY_PLAN
STORY_VALIDATE
VIDEO_RENDER
```

A job should have:

```text
id
projectId
mediaId where applicable
type
status
progress
attemptCount
createdAt
startedAt
completedAt
errorCode
errorMessage
```

States:

```text
QUEUED
RUNNING
COMPLETED
FAILED
CANCELLED
```

## 10.1 Idempotency

Every stage must be safely retryable.

Example:

If transcription succeeds but video understanding fails:

```text
DO:
retry VIDEO_UNDERSTAND

DO NOT:
rerun transcription unnecessarily
```

If VideoAnalysis exists:

```text
reuse it when the source and analysis version are identical
```

If rendering fails:

```text
retry rendering
do not rerun AI analysis
```

---

# 11. Video Intelligence Worker

The Video Intelligence Worker is responsible for turning raw video into structured evidence.

Pipeline:

```text
S3 source video
      ↓
FFprobe
      ↓
metadata
      ↓
scene detection
      ↓
candidate segments
      ↓
representative frame selection
      ↓
deduplication
      ↓
audio extraction
      ↓
transcription
      ↓
multimodal AI
      ↓
VideoAnalysis
```

## 11.1 FFmpeg / FFprobe

Use FFprobe for reliable media metadata.

Use FFmpeg for media preprocessing.

Do not duplicate media-processing logic unnecessarily between iOS and backend.

Backend video preprocessing should be the canonical processing path.

## 11.2 Frame selection

Do not rely exclusively on fixed-interval sampling.

Candidate frame selection should consider:

- scene boundaries
- action changes
- people entering/leaving
- environmental changes
- visually distinct frames
- useful timestamps
- redundancy

The objective is:

```text
high information
+
low redundancy
+
reasonable AI cost
```

---

# 12. Transcription

Transcription is a replaceable service.

Create a provider abstraction such as:

```text
TranscriptionProvider
```

Output should contain timestamped segments:

```json
{
  "segments": [
    {
      "start": 3.2,
      "end": 6.8,
      "text": "Let's go over there."
    }
  ]
}
```

The rest of the application must not depend on one specific transcription provider.

No transcript should be placed in SQS.

---

# 13. Multimodal video understanding

The multimodal AI layer answers:

> What is happening in this video, and which moments are meaningful for a personal memory story?

It should analyze evidence such as:

- timestamped frames
- scene boundaries
- transcript
- metadata
- quality signals

Potential providers can be changed later.

Do not tightly couple the rest of the application to a specific provider or model.

## 13.1 Important distinction

The model is NOT merely asked:

> Summarize this video.

It should identify candidate moments.

A candidate segment should contain:

```text
start
end
description
importance
visualQuality
emotionalValue
storyRelevance
recommended
reason
```

## 13.2 Memory value vs technical quality

This is a core product rule.

A technically shaky clip can be more valuable than a perfect shot.

Example:

```text
friends laughing with camera shake
```

may be more important than:

```text
perfectly stable empty street
```

The AI should preserve human/memory significance.

## 13.3 Hallucination control

The model must not invent:

- people
- actions
- locations
- events
- timestamps

Only return information supported by the supplied evidence.

Validate AI output against a strict schema.

---

# 14. VideoAnalysis

VideoAnalysis is a persistent, versioned artifact.

It should contain concepts such as:

```text
videoId
analysisVersion
provider
model
duration
scenes
transcript
segments
detectedPeople
detectedActivities
detectedLocations
qualitySignals
```

A segment:

```text
segmentId
start
end
description
importance
visualQuality
emotionalValue
storyRelevance
recommended
reasons
```

## 14.1 Immutability

Do not silently overwrite old analysis.

If analysis is rerun with a new model/version:

```text
VideoAnalysisV1
VideoAnalysisV2
...
```

The StoryPlan must explicitly reference the analysis version it used.

---

# 15. Story Planner

The Story Planner is an editorial layer.

It receives structured analysis.

It does NOT analyze raw video.

Inputs can include:

- PhotoAnalysis
- VideoAnalysis
- media clusters
- timestamps
- locations
- user-selected media
- requested duration
- story type
- visual style
- optional user instruction

Its job is:

> Decide which meaningful moments form the best coherent memory story.

Possible story types:

```text
Today
Weekend
Trip
Birthday
Family
Friends
Pet
Event
Custom
```

## 15.1 Story selection principles

Prefer:

- meaningful people
- interactions
- emotional moments
- memorable activities
- visually strong scenes
- variety
- coherent pacing

Avoid:

- repeated clips
- repeated photos
- technically perfect but meaningless footage
- excessive transitions
- unnecessary text

## 15.2 Chronology

Chronological ordering is the default for everyday memories, but it is not an absolute rule.

A stronger narrative may justify a different order.

---

# 16. StoryPlan

StoryPlan is a structured, renderer-independent artifact.

It should specify:

- selected assets
- exact video time ranges
- order
- clip duration
- transitions
- music configuration
- text/captions
- aspect ratio
- target duration
- pacing/style

Every selected video segment must reference:

```text
mediaId
VideoAnalysis version
valid start/end timestamps
```

Never invent an asset or timestamp.

StoryPlan must not contain renderer-specific implementation details unless they are part of a deliberately stable rendering contract.

---

# 17. StoryPlan Validator

Before rendering, validate deterministically.

Check:

1. Asset exists.
2. Asset belongs to the authenticated user/project.
3. VideoAnalysis version exists.
4. Timestamp is within source duration.
5. `start < end`.
6. S3 object exists or is resolvable.
7. Total duration is within allowed range.
8. Aspect ratio is supported.
9. Text lengths are valid.
10. Audio configuration is valid.
11. Schema is valid.

The validator must NOT use AI.

Invalid StoryPlans must never reach rendering.

---

# 18. Render Worker

The Render Worker is deterministic.

Technology:

- Node.js
- TypeScript
- Remotion
- FFmpeg
- S3

It receives:

```text
Validated StoryPlan
+
source media
```

and produces:

```text
final MP4
```

The renderer does NOT:

- analyze video
- call multimodal AI
- choose story moments
- change the StoryPlan
- decide which media is important

## 18.1 Supported output

Initial aspect ratios:

```text
9:16
1:1
16:9
```

Primary FrameMio output:

```text
9:16
```

## 18.2 Rendering features

Photos:

- pan
- zoom
- Ken Burns-style motion

Videos:

- precise trimming
- crop
- scale
- optional speed adjustment
- audio handling

Transitions:

- cut
- fade
- dissolve

Audio:

- background music
- volume control
- ducking
- fade in/out
- original audio where appropriate

Text:

- title
- captions
- optional chapter text
- ending

---

# 19. Rendering performance

Rendering can be expensive.

Do not solve slow rendering by moving rendering into the synchronous Go API.

Correct:

```text
API
 ↓
SQS
 ↓
Render Worker
 ↓
Remotion/FFmpeg
```

The user sees asynchronous progress.

Rendering workers can later scale horizontally based on queue depth.

If rendering is slow, first investigate:

- unnecessary re-encoding
- source resolution
- frame rate
- composition complexity
- unnecessary asset preprocessing
- FFmpeg parameters
- worker CPU/memory
- downloading the same asset repeatedly
- redundant intermediate files

Do not replace Remotion with another architecture merely because one render is slow unless benchmarking demonstrates the need.

---

# 20. AWS deployment

Intended deployment:

```text
Route 53
   ↓
ALB
   ↓
ECS Fargate Go API
```

Workers:

```text
ECS Fargate Video Worker
ECS Fargate Render Worker
```

Messaging:

```text
SQS
```

Storage:

```text
S3
```

Database:

```text
PostgreSQL
```

Secrets:

```text
AWS Secrets Manager
```

Logs:

```text
CloudWatch
```

## 20.1 Public endpoint

Only the API needs a public HTTPS endpoint.

Example:

```text
https://api.framemio.com
```

Workers should remain private.

---

# 21. Security requirements

Never put these in the iOS app:

- AWS secret access keys
- database passwords
- server-side AI provider secrets
- Stripe/other server secrets
- privileged internal API keys

The iPhone receives only what it needs.

S3 buckets containing user media must remain private.

Use presigned URLs for controlled upload/download.

Every API resource must enforce ownership.

Every expensive operation must be rate limited.

Especially protect:

```text
upload-session
analyze
render
```

because they create infrastructure/AI cost.

---

# 22. Database principles

PostgreSQL stores structured state.

Store:

- users
- projects
- media metadata
- jobs
- analysis metadata
- VideoAnalysis structured data
- StoryPlan
- render records
- ownership relationships

Do not store large raw videos in PostgreSQL.

Do not use PostgreSQL as a replacement for S3.

Use S3 for:

- source videos
- source photos when uploaded
- frame artifacts
- audio artifacts
- large analysis artifacts
- rendered videos

---

# 23. End-to-end project lifecycle

A typical project should follow:

```text
1. User opens FrameMio

2. User chooses "Make My Day"

3. iOS identifies candidate media

4. iOS performs lightweight local analysis

5. iOS requests upload session

6. iOS uploads media directly to S3

7. Go API creates analysis jobs

8. SQS distributes jobs

9. Video Worker preprocesses videos

10. Transcription runs when useful

11. Multimodal AI creates VideoAnalysis

12. Story Planner creates StoryPlan

13. StoryPlan Validator validates it

14. Render Worker renders MP4

15. Final video is uploaded to S3

16. iPhone receives completion status

17. User watches video

18. User can:
    - regenerate
    - remove moment
    - replace moment
    - make shorter
    - make longer
    - change music
    - share
```

---

# 24. Regeneration rules

This is important for cost and reliability.

## If user says "regenerate story"

Do NOT necessarily rerun video understanding.

Use existing:

```text
VideoAnalysis
```

and create a new:

```text
StoryPlan
```

## If user says "render again"

Do not rerun:

- video analysis
- transcription
- story planning

Use the existing validated StoryPlan.

## If the user explicitly asks to reanalyze

Then create a new analysis version.

---

# 25. Debugging rules for Codex

When a test fails, follow this order.

## Step 1 — Identify the layer

Classify the failure as one of:

```text
iOS
API
Authentication
Authorization
S3
SQS
PostgreSQL
Video preprocessing
Transcription
AI provider
VideoAnalysis
Story Planner
StoryPlan validation
Rendering
Deployment/networking
```

## Step 2 — Reproduce

Do not guess.

Find:

- exact error
- request
- response
- relevant logs
- job state
- AWS resource state
- database state

## Step 3 — Trace the contract

Ask:

```text
What does the upstream layer produce?
What does the downstream layer expect?
```

Many integration errors are contract mismatches.

Check:

- JSON shape
- IDs
- timestamps
- enum values
- authentication
- S3 keys
- queue message format
- database schema
- API response shape

## Step 4 — Fix the owner

Fix the layer responsible for violating the contract.

Do not add random compatibility code everywhere.

## Step 5 — Retest

Run the smallest relevant test first.

Then run integration tests.

Then run the complete pipeline if necessary.

---

# 26. Common wrong directions to avoid

## Do not make Go API process videos synchronously

Wrong:

```text
HTTP request
 ↓
FFmpeg
 ↓
AI
 ↓
Remotion
 ↓
HTTP response
```

Correct:

```text
HTTP
 ↓
job
 ↓
SQS
 ↓
worker
```

## Do not upload videos through Go

Wrong:

```text
iPhone
 ↓
Go API
 ↓
S3
```

Correct:

```text
iPhone
 ↓
presigned URL
 ↓
S3
```

## Do not put large objects into SQS

Wrong:

```text
SQS
 ↓
video bytes
```

Correct:

```text
SQS
 ↓
mediaId / jobId / projectId
 ↓
S3
```

## Do not perform full video understanding on iPhone

Wrong:

```text
iPhone
 ↓
large frame extraction
 ↓
multimodal AI
```

Correct:

```text
iPhone
 ↓
lightweight metadata
```

then:

```text
S3
 ↓
Video Worker
 ↓
AI
```

## Do not let AI directly control Remotion

Wrong:

```text
AI
 ↓
arbitrary Remotion code
```

Correct:

```text
AI
 ↓
structured StoryPlan
 ↓
validator
 ↓
Remotion
```

## Do not mix VideoAnalysis and StoryPlan

VideoAnalysis describes evidence.

StoryPlan makes editorial decisions.

## Do not make every worker publicly accessible

Only the Go API needs the public API endpoint.

---

# 27. AI provider abstraction

AI providers must be replaceable.

Use interfaces similar to:

```text
VideoUnderstandingProvider
TranscriptionProvider
StoryPlanningProvider
```

Provider-specific request/response logic stays inside adapters.

The core domain model must not depend on:

- one vendor
- one model name
- one SDK response shape

Store provider/model/version metadata with AI-generated artifacts.

---

# 28. Versioning

Version all important contracts.

Examples:

```text
VideoAnalysisV1
StoryPlanV1
RenderSchemaV1
```

Also track:

```text
analysisVersion
promptVersion
model
rendererVersion
```

This is essential because AI output changes over time.

A render should be reproducible from:

```text
StoryPlan version
+
source asset versions
+
renderer version
```

---

# 29. Observability

Every asynchronous operation should have a traceable ID.

At minimum:

```text
requestId
projectId
jobId
mediaId
```

Logs should make it possible to trace:

```text
iPhone request
 ↓
API request
 ↓
job creation
 ↓
SQS message
 ↓
worker execution
 ↓
VideoAnalysis
 ↓
StoryPlan
 ↓
render
 ↓
final S3 object
```

Avoid logging:

- authentication tokens
- AWS secrets
- AI API keys
- unnecessary private user content

---

# 30. Error handling

Errors should be categorized.

Example:

```text
AUTHENTICATION_ERROR
AUTHORIZATION_ERROR
UPLOAD_ERROR
S3_ERROR
QUEUE_ERROR
MEDIA_DECODE_ERROR
TRANSCRIPTION_ERROR
AI_PROVIDER_ERROR
AI_SCHEMA_ERROR
STORY_PLAN_ERROR
VALIDATION_ERROR
RENDER_ERROR
```

The iOS app should receive user-friendly states.

Do not expose raw infrastructure errors to the user.

For example, instead of:

```text
NoSuchKey: s3://bucket/...
```

show:

```text
We couldn't access one of your videos. Please try again.
```

But keep the detailed error in backend logs.

---

# 31. Testing strategy

Test at four levels.

## Unit tests

Test:

- domain logic
- validators
- parsers
- StoryPlan validation
- ownership checks
- timestamp validation

## Integration tests

Test:

```text
Go API
 ↔ PostgreSQL
 ↔ S3
 ↔ SQS
```

and worker components.

## Media pipeline tests

Use a small fixed set of real representative videos:

- short video
- long video
- silent video
- speech video
- multiple scenes
- static scene
- fast motion
- low-quality footage
- portrait iPhone video

## End-to-end test

Test:

```text
upload
 ↓
preprocess
 ↓
transcribe
 ↓
understand
 ↓
story plan
 ↓
validate
 ↓
render
 ↓
download/play
```

---

# 32. Definition of "fixed"

A bug is not fixed merely because one exception disappeared.

A fix is complete when:

1. Root cause is understood.
2. The responsible layer is corrected.
3. The relevant contract remains valid.
4. Regression test exists where appropriate.
5. Retry/idempotency behavior remains correct.
6. No unrelated architecture was changed.
7. The end-to-end pipeline still works.

---

# 33. Codex operating procedure

When asked to modify FrameMio, Codex should first:

```text
1. Inspect repository structure.

2. Inspect existing architecture and implementation.

3. Inspect relevant tests.

4. Inspect environment/configuration names.

5. Inspect AWS integration code.

6. Identify the failing layer.

7. Compare implementation against this document.

8. Fix the smallest responsible component.

9. Preserve existing API/database/message contracts unless they are demonstrably wrong.

10. Add/update tests.

11. Run targeted tests.

12. Run broader tests.

13. Report:
    - root cause
    - files changed
    - why the fix is correct
    - tests executed
    - remaining issues
```

Do not blindly rewrite working code.

---

# 34. Architecture change policy

A proposed architecture change must answer:

### What is currently broken?

### Why can't the existing architecture solve it?

### What component should own the new responsibility?

### What contracts change?

### What migration is required?

### What are the operational/cost implications?

### What tests prove the new design works?

If those questions cannot be answered, prefer a local implementation fix.

---

# 35. Current project priority

The current priority is NOT adding large new features.

Priority order:

```text
1. Make the existing architecture correct.

2. Make the end-to-end pipeline reliable.

3. Fix integration contracts.

4. Make uploads reliable.

5. Make async jobs reliable.

6. Make AI outputs schema-safe.

7. Make StoryPlan deterministic and valid.

8. Make rendering reliable.

9. Improve performance.

10. Improve UX.

11. Add additional product features.
```

Do not expand the feature surface while the core pipeline is unstable.

---

# 36. Product-level success criteria

The first meaningful milestone is:

```text
A real iPhone video
       ↓
FrameMio
       ↓
S3
       ↓
Video Intelligence
       ↓
VideoAnalysis
       ↓
StoryPlan
       ↓
Validator
       ↓
Remotion
       ↓
final MP4
       ↓
iPhone playback
```

The system should successfully do this repeatedly.

After that, optimize:

- processing speed
- AI cost
- render time
- battery use
- upload reliability
- memory usage
- user experience

---

# 37. Final guardrails for Codex

Before making a change, ask:

1. **Which architectural layer owns this responsibility?**
2. **Am I fixing the root cause or masking a symptom?**
3. **Am I accidentally moving server work onto iOS?**
4. **Am I accidentally making the Go API synchronous?**
5. **Am I putting large data into SQS/PostgreSQL/API requests?**
6. **Am I bypassing S3 for large media?**
7. **Am I mixing VideoAnalysis with StoryPlan?**
8. **Am I letting AI directly control rendering?**
9. **Am I breaking idempotency?**
10. **Am I creating a public endpoint unnecessarily?**
11. **Am I introducing a provider-specific dependency into the core domain?**
12. **Can this be fixed with a smaller change?**
13. **What regression test proves the fix?**

If the answer to any of these indicates architectural drift, stop and reconsider the implementation.

---

# 38. One-sentence architecture summary

**FrameMio is an iPhone-first AI memory filmmaker where the iPhone performs lightweight media intelligence, S3 stores media, Go orchestrates authenticated asynchronous jobs, workers perform video preprocessing and AI understanding, VideoAnalysis represents evidence, StoryPlan represents editorial decisions, and Remotion/FFmpeg deterministically renders the final film.**
