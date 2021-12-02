#!/bin/bash

# Set up the routing needed for the simulation.
/setup.sh

export RTPLOGDIR=/log
export CCLOGFILE=/log/cc.log
export QLOGDIR=/log
export STREAMLOGFILE=/log/stream.log

if [ "$ROLE" == "sender" ]; then
    echo "Starting RTQ sender..."
    QUIC_GO_LOG_LEVEL=error ./rtq send -addr $RECEIVER:4242 $ARGS /input/input.y4m
else
    echo "Running RTQ receiver."
    QUIC_GO_LOG_LEVEL=error ./rtq receive $ARGS /output/out.y4m
fi
