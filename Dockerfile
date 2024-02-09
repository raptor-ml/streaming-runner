ARG LDFLAGS
ARG VERSION

### Build
FROM golang:1.19 AS build
ARG TARGETOS
ARG TARGETARCH
ARG LDFLAGS

WORKDIR /workspace
COPY go.mod /workspace
COPY go.sum /workspace
RUN go mod download
COPY . /workspace

### Core
FROM build AS build
RUN CGO_ENABLED=0 go build -ldflags="${LDFLAGS}" -o /out/runner cmd/streaming/*.go

FROM gcr.io/distroless/static:nonroot as runner
ARG VERSION

LABEL org.opencontainers.image.source="https://github.com/raptor-ml/streaming-runner"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.url="https://raptor.ml"
LABEL org.opencontainers.image.title="Raptor - Streaming Runner"
LABEL org.opencontainers.image.description="The streaming runner is responsible to run the streaming jobs."

WORKDIR /
COPY --from=build /out/runner .
USER 65532:65532

ENTRYPOINT ["/runner"]

