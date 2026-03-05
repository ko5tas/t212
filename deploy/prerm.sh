#!/bin/sh
set -e

case "$1" in
    remove|deconfigure)
        if [ -d /run/systemd/system ]; then
            systemctl stop t212 || true
            systemctl disable t212 || true
        fi
        ;;
    upgrade)
        # Don't stop/disable on upgrade — postinst will restart
        ;;
esac
