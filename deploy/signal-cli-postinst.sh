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
        ;;
esac
