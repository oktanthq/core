FROM golang:1.26.3 AS build

WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/core ./cmd/core

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/core /core
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/core"]
