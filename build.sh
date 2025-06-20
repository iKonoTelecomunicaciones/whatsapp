#!/bin/sh
if ! [ $(git config --global --get safe.directory) ]; then
    echo "Setting safe.directory config to /build"
    git config --global --add safe.directory /build
fi
MAUTRIX_NAME='github.com/iKonoTelecomunicaciones/go'
MAUTRIX_VERSION=$(cat go.mod | grep $MAUTRIX_NAME | awk '{ print $2 }' | head -n1)
GO_LDFLAGS="
    -s -w \
    -X main.Tag=$(git describe --exact-match --tags 2>/dev/null) \
    -X main.Commit=$(git rev-parse HEAD) \
    -X 'main.BuildTime=`date -Iseconds`' \
    -X '$MAUTRIX_NAME.GoModVersion=$MAUTRIX_VERSION' \
"
go clean -modcache
go build -ldflags="$GO_LDFLAGS" "$@" ./cmd/mautrix-whatsapp
