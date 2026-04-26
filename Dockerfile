FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/livart ./cmd/livart

FROM alpine:3.22

RUN adduser -D -H livart
WORKDIR /app
COPY --from=build /out/livart /usr/local/bin/livart
RUN mkdir -p /app/data/objects && chown -R livart:livart /app
USER livart
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/livart"]
