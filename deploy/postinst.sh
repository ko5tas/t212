#!/bin/sh
set -e

case "$1" in
    configure)
        # Create system user if not already present
        if ! id -u t212 >/dev/null 2>&1; then
            useradd --system --no-create-home --shell /usr/sbin/nologin t212
        fi

        # Secure the config directory
        chown root:root /etc/t212
        chmod 700 /etc/t212

        # Secure the config file (nfpm installs it; we set ownership and mode here)
        chown root:root /etc/t212/config.env
        chmod 0600 /etc/t212/config.env

        # Register and enable the service (does NOT start it)
        if [ -d /run/systemd/system ]; then
            systemctl daemon-reload
            systemctl enable t212
        fi

        echo ""
        echo "================================================================"
        echo " t212 installed successfully."
        echo "================================================================"
        echo ""
        echo " Next steps:"
        echo ""
        echo "  1. Edit the config file and set your credentials:"
        echo "       sudo nano /etc/t212/config.env"
        echo ""
        echo "     Required:"
        echo "       T212_API_KEY=<your Trading 212 live API key>"
        echo ""
        echo "     Optional:"
        echo "       SIGNAL_NUMBER=+447700000000   (Signal notifications)"
        echo "       T212_PORT=8080                (web UI port, default: 8080)"
        echo ""
        echo "  2. Start the service:"
        echo "       sudo systemctl start t212"
        echo ""
        echo "  3. Check it is running:"
        echo "       sudo systemctl status t212"
        echo "       sudo journalctl -u t212 -f"
        echo ""
        echo "  4. Open the web UI in a browser:"
        echo "       http://localhost:8080        (from this machine)"
        echo "       http://<this-host-ip>:8080   (from another device on the LAN)"
        echo ""
        echo "================================================================"
        echo ""
        ;;
esac
