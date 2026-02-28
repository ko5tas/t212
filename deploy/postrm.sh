#!/bin/sh
set -e

case "$1" in
    purge)
        rm -rf /etc/t212

        if id -u t212 >/dev/null 2>&1; then
            userdel t212 || true
        fi
        ;;
esac
