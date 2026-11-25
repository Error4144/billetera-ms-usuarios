FROM golang:1.27.4-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -o /ms-usuarios ./cmd/ms-usuarios

FROM scratch
COPY --from=build /ms-usuarios /ms-usuarios
USER 65532:65532
ENTRYPOINT ["/ms-usuarios"]
