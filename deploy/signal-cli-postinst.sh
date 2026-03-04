#!/bin/sh
set -e

case "$1" in
    configure)
        # Check for Java runtime
        if ! command -v java >/dev/null 2>&1; then
            echo ""
            echo "================================================================"
            echo " WARNING: Java JRE not found."
            echo " signal-cli requires Java 17+."
            echo " Install via: dietpi-software install 196"
            echo " Or:          sudo apt install openjdk-17-jre-headless"
            echo "================================================================"
            echo ""
        fi
        ;;
esac
