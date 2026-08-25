FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/relayshelf ./cmd/relayshelf

FROM gcr.io/distroless/base-debian13:nonroot
COPY --from=build /out/relayshelf /relayshelf
EXPOSE 8080
ENTRYPOINT ["/relayshelf"]
