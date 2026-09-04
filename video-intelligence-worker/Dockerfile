FROM golang:1.24-bookworm AS build

WORKDIR /src/video-intelligence-worker

COPY video-intelligence-worker/go.mod video-intelligence-worker/go.sum ./
RUN go mod download

COPY video-intelligence-worker/. ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/video-intelligence-worker ./cmd/worker

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg imagemagick \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --uid 10001 framemio
USER framemio
WORKDIR /home/framemio

COPY --from=build /out/video-intelligence-worker /usr/local/bin/video-intelligence-worker

ENTRYPOINT ["/usr/local/bin/video-intelligence-worker"]
