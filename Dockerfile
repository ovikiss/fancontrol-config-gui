FROM golang:1.24-alpine AS build
ARG UI_SHARED_REV=main
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum main.go ./
COPY scripts/fancontrol-gui.sh scripts/fancontrol-gui.service ./scripts/
RUN git clone --depth 1 https://github.com/ovikiss/mikrotik-ui-shared.git /tmp/mikrotik-ui-shared \
    && git -C /tmp/mikrotik-ui-shared fetch --depth 1 origin "$UI_SHARED_REV" \
    && git -C /tmp/mikrotik-ui-shared checkout --detach "$UI_SHARED_REV"
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /fancontrol-config-gui .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates openssh-client
WORKDIR /app
COPY --from=build /fancontrol-config-gui /usr/local/bin/fancontrol-config-gui
COPY index.html app.js styles.css header-controls.json /app/
COPY --from=build /tmp/mikrotik-ui-shared/ui /opt/mikrotik-ui-shared/ui
ENV APP_DIR=/app \
    SETTINGS_FILE=/data/settings.json \
    UI_SHARED_DIR=/opt/mikrotik-ui-shared/ui \
    PORT=4173
VOLUME ["/data"]
EXPOSE 4173
ENTRYPOINT ["/usr/local/bin/fancontrol-config-gui"]
