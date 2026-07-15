FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /webhook ./cmd/webhook

FROM gcr.io/distroless/static:nonroot
COPY --from=build /webhook /webhook
# numeric USER: required for runAsNonRoot kubelet verification
USER 65532:65532
ENTRYPOINT ["/webhook"]
