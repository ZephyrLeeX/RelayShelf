FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/share-system ./cmd/share-system

FROM gcr.io/distroless/base-debian13:nonroot
COPY --from=build /out/share-system /share-system
EXPOSE 8080
ENTRYPOINT ["/share-system"]
