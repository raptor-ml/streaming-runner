### Streaming-runner
FROM gcr.io/distroless/static:nonroot as streaming
WORKDIR /
COPY bin/runner .
USER 65532:65532

ENTRYPOINT ["/runner"]
