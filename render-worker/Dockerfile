FROM node:22-bookworm-slim AS build

WORKDIR /src/render-worker

COPY render-worker/package*.json ./
RUN npm ci

COPY render-worker/tsconfig.json ./
COPY render-worker/src ./src
RUN npm run build

FROM node:22-bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends awscli ca-certificates chromium ffmpeg fonts-liberation fonts-noto-core libheif-examples \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app
ENV NODE_ENV=production

COPY render-worker/package*.json ./
RUN npm ci

COPY --from=build /src/render-worker/src ./src
COPY --from=build /src/render-worker/tsconfig.json ./tsconfig.json

CMD ["npm", "start"]
