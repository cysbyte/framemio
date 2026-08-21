FROM golang:1.24-bookworm AS build

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/. ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/backend ./cmd/api

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --uid 10001 framemio
USER framemio
WORKDIR /home/framemio

COPY --from=build /out/backend /usr/local/bin/backend

ENTRYPOINT ["/usr/local/bin/backend"]
