# Build a static CloudRipper binary and run it on a distroless image.
FROM golang:1.22-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cloudripper .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cloudripper /cloudripper
USER nonroot:nonroot
ENTRYPOINT ["/cloudripper"]
