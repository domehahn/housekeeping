# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w \
      -X github.com/domehahn/housekeeping/pkg/version.Version=${VERSION} \
      -X github.com/domehahn/housekeeping/pkg/version.Commit=${COMMIT} \
      -X github.com/domehahn/housekeeping/pkg/version.BuildDate=${BUILD_DATE}" \
    -o /out/scm-cleaner ./cmd/scm-cleaner

# ---- final image: no shell, no build tools, non-root ----------------------
FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/scm-cleaner /usr/local/bin/scm-cleaner

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/scm-cleaner"]
CMD ["--help"]
