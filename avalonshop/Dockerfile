FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /avalonshop . \
 && mkdir -p /data/uploads

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /avalonshop /avalonshop
COPY --from=build --chown=nonroot:nonroot /data/uploads /data/uploads
ENV UPLOAD_DIR=/data/uploads ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 CMD ["/avalonshop", "-healthcheck"]
ENTRYPOINT ["/avalonshop"]