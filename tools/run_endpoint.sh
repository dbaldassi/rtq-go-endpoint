#!/bin/bash
set -e

# Set up the routing needed for the simulation.
/setup.sh

if [ "$ROLE" == "sender" ]; then
    # Wait for the simulator to start up.
    /wait-for-it.sh sim:57832 -s -t 10
    echo "Starting RTQ sender..."
    echo "Sending video to $RECEIVER"
    echo "Sender params: $SENDER_PARAMS"
    QUIC_GO_LOG_LEVEL=debug ./sender -transport udp -remote $RECEIVER $SENDER_PARAMS $VIDEOS
else
    echo "Running RTQ receiver."
    QUIC_GO_LOG_LEVEL=debug ./receiver -transport udp $DESTINATION
fi
