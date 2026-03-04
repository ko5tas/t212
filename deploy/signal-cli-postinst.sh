#!/bin/sh
set -e

case "$1" in
    configure)
        # Check for Java runtime
        if ! command -v java >/dev/null 2>&1; then
            echo ""
            echo "================================================================"
            echo " WARNING: Java JRE not found."
            echo " signal-cli requires Java 25+."
            echo " Install via: sudo apt install openjdk-25-jre-headless"
            echo "================================================================"
            echo ""
        fi

        # Create signal-cli data directory if the t212 service user exists.
        # This ensures the directory is created regardless of whether
        # signal-cli or t212 is installed first.
        if id -u t212 >/dev/null 2>&1; then
            mkdir -p /var/lib/t212/signal-cli
            chown t212:t212 /var/lib/t212/signal-cli
        fi
        ;;
esac
