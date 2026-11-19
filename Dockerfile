# syntax=docker/dockerfile:1

FROM rust:alpine AS build
WORKDIR /src

RUN apk add --no-cache musl-dev ca-certificates

COPY Cargo.toml Cargo.lock ./
COPY src ./src

RUN cargo build --release
RUN cp target/release/scm-cleaner /usr/local/bin/scm-cleaner

# ---- final image: minimal, non-root, scratch ----------------------
FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/local/bin/scm-cleaner /usr/local/bin/scm-cleaner

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/scm-cleaner"]
CMD ["--help"]
