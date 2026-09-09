# Build context: wasmCloud v2.8.0, commit 5c4ec4a3d008b3f401d9e763515f434deebc9936.
# Retain upstream default capabilities and add the opt-in WASI TLS implementation.
FROM cgr.dev/chainguard/rust:latest-dev@sha256:74a141a28f209de3543f987ba77bb5b08a8ea13103ec39c18053bec3e4cc6441 AS builder
WORKDIR /src
USER root
RUN apk --no-cache add protoc protobuf protobuf-dev
USER nonroot
COPY --chown=nonroot:nonroot . .
RUN cargo build --locked --release --bin wash --features wasi-tls

FROM cgr.dev/chainguard/wolfi-base@sha256:918a593b8268c222afd4e2c4f06860ac984e60719b4697e4c71d796bc8fcd042
RUN apk add --no-cache git
COPY --from=builder /src/target/release/wash /usr/local/bin/wash
ENTRYPOINT ["/usr/local/bin/wash"]
