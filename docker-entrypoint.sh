#!/bin/sh
# Refuse to start without a device to point at.
#
# Compose interpolates the whole file even for inactive profiles, so the
# requirement cannot live in the compose file without breaking the demo profile.
# It lives here instead: the demo and the simulator set the variable, an
# operator starting the real service must too.
set -eu

: "${FRONIUS_SOLARAPI_URL:?set FRONIUS_SOLARAPI_URL to your inverter, e.g. http://192.168.1.50}"

exec "$@"
