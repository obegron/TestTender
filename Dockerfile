FROM docker.io/library/golang:1.26.6-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG BUILD=unknown
ARG BUILD_DATE=unknown
ARG IMAGE=ghcr.io/obegron/testtender:dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w \
      -X github.com/obegron/testtender/internal/config.Version=${VERSION} \
      -X github.com/obegron/testtender/internal/config.Build=${BUILD} \
      -X github.com/obegron/testtender/internal/config.Date=${BUILD_DATE} \
      -X github.com/obegron/testtender/internal/config.Image=${IMAGE}" \
    -o /out/testtender .

FROM docker.io/library/alpine:3.23

RUN apk add --no-cache ca-certificates \
    && addgroup -g 65532 testtender \
    && adduser -D -H -u 65532 -G testtender testtender
COPY --from=build /out/testtender /usr/local/bin/testtender

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/testtender"]
CMD ["server"]
