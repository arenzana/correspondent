# syntax=docker/dockerfile:1
#
# Correspondent: one static binary with the reader UI embedded, on distroless.
# Base images are pinned by digest; Dependabot proposes updates.

FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:40cd011b55415bc894e45b02fdd5a5dfde056921b7d23a64c74f80497af48797 AS ui
WORKDIR /src/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci --ignore-scripts
COPY ui/ ./
RUN npm run check && npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS go
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
COPY --from=ui /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${VERSION}" -o /out/correspondent ./cmd/correspondent \
 && mkdir -p /out/data

# Distroless static: no shell, no package manager, CA roots and tzdata only.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
ARG VERSION=dev
LABEL org.opencontainers.image.title="Correspondent" \
      org.opencontainers.image.description="Self-hosted feed reader with a Reeder-compatible API" \
      org.opencontainers.image.source="https://github.com/arenzana/correspondent" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=go /out/correspondent /correspondent
# The data directory must exist, owned by the nonroot user, so a fresh named
# volume inherits the right ownership.
COPY --from=go --chown=65532:65532 /out/data /data
ENV CORRESPONDENT_DATA=/data \
    CORRESPONDENT_ADDR=:8080
VOLUME /data
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/correspondent", "healthcheck"]
ENTRYPOINT ["/correspondent"]
CMD ["serve"]
