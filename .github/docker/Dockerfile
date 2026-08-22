# FG-QiMen OCI image — minimal scratch-based image.
#
# Multi-stage build:
#   1. golang:1.26 — compile statically with CGO disabled
#   2. gcr.io/distroless/static:nonroot — minimal runtime
#
# The final image is ~10 MB and runs as UID 65532 (nonroot).
#
# / FG-QiMen OCI 镜像——基于 scratch 的最小镜像。
# 多阶段构建：
#   1. golang:1.26 — 静态编译，禁用 CGO
#   2. gcr.io/distroless/static:nonroot — 最小运行时
# 最终镜像 ~10 MB，以 UID 65532（nonroot）运行。

# ---- Stage 1: build -----------------------------------------------
FROM golang:1.26 AS build

WORKDIR /src

# Reproducible-build env. / 可复现构建环境变量。
ENV SOURCE_DATE_EPOCH=1700000000 \
    CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN go build -trimpath -buildvcs=false \
    -ldflags "-s -w -buildid= -X github.com/LCUstinian/FG-QiMen/internal/version.Value=v${VERSION}" \
    -o /out/fg-qimen \
    .

# ---- Stage 2: runtime ---------------------------------------------
FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/fg-qimen /usr/local/bin/fg-qimen

# OCI image labels. / OCI 镜像标签。
LABEL org.opencontainers.image.title="fg-qimen" \
      org.opencontainers.image.description="Pipeline scanner with project workspaces" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/LCUstinian/FG-QiMen"

ENTRYPOINT ["/usr/local/bin/fg-qimen"]