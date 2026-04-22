# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -tags timetzdata -ldflags="-s -w" \
    -o /out/presence ./cmd/presence

# Create the data directory owned by the nonroot user so that when Docker
# initialises a fresh named volume at /data it inherits the right ownership
# and the distroless runtime (UID 65532) can create the SQLite file.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/presence /presence
COPY --from=builder --chown=65532:65532 /out/data /data

USER 65532:65532
ENTRYPOINT ["/presence"]
