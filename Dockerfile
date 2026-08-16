FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X 'main.version=${VERSION}' -X 'main.gitCommit=${GIT_COMMIT}' -X 'main.buildTime=${BUILD_TIME}'" -o /out/sidewhale .

FROM debian:trixie-slim AS proot-build
ARG PROOT_VERSION=v5.4.0
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    make \
    build-essential \
    pkg-config \
    libarchive-dev \
    libtalloc-dev \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /tmp
RUN curl -fsSL "https://codeload.github.com/proot-me/proot/tar.gz/refs/tags/${PROOT_VERSION}" -o proot.tar.gz \
    && mkdir proot-src \
    && tar -xzf proot.tar.gz -C proot-src --strip-components=1 \
    && make -C proot-src/src loader.elf build.h \
    && make -C proot-src/src proot \
    && mkdir -p /out \
    && install -m 0755 proot-src/src/proot /out/proot

FROM debian:trixie-slim AS runtime-deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libtalloc2 \
	&& rm -rf /var/lib/apt/lists/* \
	&& mkdir -p /out \
	&& cp --parents /usr/lib/*-linux-gnu/libtalloc.so.2 /out

FROM gcr.io/distroless/cc-debian13:nonroot
COPY --from=build /out/sidewhale /sidewhale
COPY --from=proot-build /out/proot /usr/local/bin/proot
COPY --from=runtime-deps /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=runtime-deps /out/usr/lib/ /usr/lib/
EXPOSE 23750
ENTRYPOINT ["/sidewhale"]
