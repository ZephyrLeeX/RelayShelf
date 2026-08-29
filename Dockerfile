FROM docker.io/library/node:24.5.0 AS web-build
RUN corepack enable && corepack prepare pnpm@10.15.1 --activate
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM docker.io/library/golang:1.26.0 AS build
# Release metadata is injected explicitly. The build context excludes .git, so
# Go VCS stamping is unavailable here and an unset build arg stays empty rather
# than being replaced by a fabricated version.
ARG VERSION=""
ARG GIT_COMMIT=""
ARG BUILD_TIME=""
WORKDIR /src
COPY go.mod go.sum ./
# Generator tools are pinned by go.mod but are not part of the production
# binary. Download only runtime roots here; go build resolves their small
# transitive closure without pulling sqlc/oapi-codegen's build-only graph.
RUN go mod download github.com/go-chi/chi/v5 github.com/google/uuid github.com/jackc/pgx/v5 github.com/oapi-codegen/runtime golang.org/x/crypto golang.org/x/image
COPY . .
RUN rm -rf ./internal/webui/dist && mkdir -p ./internal/webui/dist
COPY --from=web-build /src/web/dist ./internal/webui/dist
RUN go build \
  -ldflags="-X github.com/ZephyrLeeX/RelayShelf/internal/platform/buildinfo.Version=${VERSION} \
    -X github.com/ZephyrLeeX/RelayShelf/internal/platform/buildinfo.GitCommit=${GIT_COMMIT} \
    -X github.com/ZephyrLeeX/RelayShelf/internal/platform/buildinfo.BuildTime=${BUILD_TIME}" \
  -o /out/relayshelf ./cmd/relayshelf

FROM gcr.io/distroless/base-debian13:nonroot
ARG VERSION=""
ARG GIT_COMMIT=""
ARG BUILD_TIME=""
LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}" \
      org.opencontainers.image.source="https://github.com/ZephyrLeeX/RelayShelf"
COPY --from=build /out/relayshelf /relayshelf
EXPOSE 8080
ENTRYPOINT ["/relayshelf"]
CMD ["serve"]
