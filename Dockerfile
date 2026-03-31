# -----------------------------------------------------------------
# Builder
# -----------------------------------------------------------------
FROM golang:1.25-alpine3.22 as builder
ARG VERSION

RUN apk add --update git

ADD ./ /src

WORKDIR /src
RUN go build -ldflags="-s -w -X github.com/webrpc/ridl-lsp.VERSION=${VERSION}" -o /usr/bin/ridl-lsp ./cmd/ridl-lsp

# -----------------------------------------------------------------
# Runner
# -----------------------------------------------------------------
FROM alpine:3.22

ENV TZ=UTC

RUN apk add --no-cache --update ca-certificates

COPY --from=builder /usr/bin/ridl-lsp /usr/bin/

ENTRYPOINT ["/usr/bin/ridl-lsp"]
