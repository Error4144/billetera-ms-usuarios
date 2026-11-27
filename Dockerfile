FROM golang:1.27.4-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
COPY gen/ ./gen/
RUN CGO_ENABLED=0 go build -o /ms-usuarios ./cmd/ms-usuarios
RUN CGO_ENABLED=0 go build -o /migrate ./cmd/migrate

FROM build AS test
COPY tests/ ./tests/
CMD ["go", "test", "-coverpkg=./internal/...,./migrations", "-coverprofile=/reports/coverage.out", "./..."]

FROM scratch
COPY --from=build /ms-usuarios /ms-usuarios
COPY --from=build /migrate /migrate
USER 65532:65532
ENTRYPOINT ["/ms-usuarios"]
