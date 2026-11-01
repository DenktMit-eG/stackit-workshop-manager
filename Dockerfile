# Build
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/stackit-ws-manager .

# Runtime: official Playwright image ships Chromium + all system deps.
# Keep the Playwright version in sync with playwright-go (see run.go playwrightCliVersion).
FROM mcr.microsoft.com/playwright:v1.62.1-noble
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
COPY --from=build /out/stackit-ws-manager /usr/local/bin/stackit-ws-manager
RUN stackit-ws-manager browser install
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["stackit-ws-manager", "--config", "/data/config.yaml"]
CMD ["--help"]
