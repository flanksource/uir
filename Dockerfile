# syntax=docker/dockerfile:1.7

FROM node:24-bookworm-slim AS web
ARG PNPM_VERSION=11.22.0
RUN npm install --global "pnpm@${PNPM_VERSION}"
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

FROM golang:1.26.1-bookworm AS build
ENV GOWORK=off
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN make binary VERSION="${VERSION}"
RUN ./.bin/uir --version

FROM debian:bookworm-slim
# git is required at runtime: indexing and source views shell out to it.
RUN apt-get update \
  && apt-get install --yes --no-install-recommends ca-certificates git \
  && rm -rf /var/lib/apt/lists/*
RUN useradd --create-home --uid 10001 uir
COPY --from=build /src/.bin/uir /usr/local/bin/uir
USER 10001:10001
WORKDIR /home/uir
ENV HOME=/home/uir
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/uir"]
CMD ["serve", "--host", "0.0.0.0", "--port", "8080"]
