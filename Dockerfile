FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
# Generator tools are pinned by go.mod but are not part of the production
# binary. Download only runtime roots here; go build resolves their small
# transitive closure without pulling sqlc/oapi-codegen's build-only graph.
RUN go mod download github.com/go-chi/chi/v5 github.com/google/uuid github.com/jackc/pgx/v5 github.com/oapi-codegen/runtime golang.org/x/crypto golang.org/x/image
COPY . .
RUN go build -o /out/relayshelf ./cmd/relayshelf

FROM gcr.io/distroless/base-debian13:nonroot
COPY --from=build /out/relayshelf /relayshelf
EXPOSE 8080
ENTRYPOINT ["/relayshelf"]
CMD ["serve"]
