#!/bin/sh
set -e

case "$1" in
    remove|upgrade|deconfigure)
        if [ -d /run/systemd/system ]; then
            systemctl stop t212 || true
            systemctl disable t212 || true
        fi
        ;;
esac
