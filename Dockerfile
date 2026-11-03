FROM golang:1.26.10@sha256:670bb5aa5349ae03b8516764718486f4f16399134bdc9acd0c46a23f043ab05c AS catalog
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN go run ./cmd/site-data > /openapi-site.json

FROM node:24.22.2-bookworm-slim@sha256:c86ecead9e55a5210ac1727de38a4ca840bf03dcaf34dc9749addbf383b1920c AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
COPY templates/ /src/templates/
COPY --from=catalog /openapi-site.json /openapi-site.json
RUN node scripts/catalog.mjs /openapi-site.json && npm run typecheck && npm run build:static

FROM catalog AS build
COPY --from=frontend /src/templates/ ./templates/
COPY --from=frontend /src/FRONTEND_NOTICES.md ./FRONTEND_NOTICES.md
RUN CGO_ENABLED=0 GOOS=linux go build -mod=readonly -trimpath -ldflags="-s -w" -o /httptoy .

FROM scratch
ARG git_sha="development"
ARG app_version="unknown"
ENV GIT_SHA=$git_sha APP_VERSION=$app_version
LABEL org.opencontainers.image.title="httptoy" \
      org.opencontainers.image.description="A powerful HTTP Request & Response Service." \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/TrueFix/httptoy"
COPY --from=build /httptoy /httptoy
COPY --from=build /src/LICENSE /licenses/httptoy-LICENSE
COPY --from=build /src/THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY --from=build /src/FRONTEND_NOTICES.md /licenses/FRONTEND_NOTICES.md
COPY --from=build /usr/local/go/LICENSE /licenses/Go-LICENSE
USER 10001:10001
EXPOSE 8000
ENTRYPOINT ["/httptoy"]
