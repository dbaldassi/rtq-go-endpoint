#!/bin/bash
set -e

# Set up the routing needed for the simulation.
#/setup.sh

mkdir -p /logs/qlog

# rtq #########################################################################

if [ "$ROLE" == "sender" ]; then
    # Wait for the simulator to start up.
    #/wait-for-it.sh sim:57832 -s -t 10
    echo "Starting RTQ sender..."
    ./link.sh $TC_CONFIG &
    tcpdump -i eth0 -l -e -n src 172.17.0.3 | ./tcpdumpbitrate.py /root/send_bandwidth.csv &
    tcpdump -i eth0 -l -e -n src 172.17.0.2 | ./tcpdumpbitrate.py /root/receive_bandwidth.csv &
    QUIC_GO_LOG_LEVEL=error ./rtq send -addr $RECEIVER $SENDER_PARAMS $VIDEOS
    ./link.sh
fi

if [ "$ROLE" == "receiver" ]
then
    echo "Running RTQ receiver."
    QUIC_GO_LOG_LEVEL=error ./rtq receive $RECEIVER_PARAMS $DESTINATION
fi

# iperf #######################################################################

if [ "$ROLE" == "sender-iperf" ]; then
    # Wait for the simulator to start up.
    #/wait-for-it.sh sim:57832 -s -t 10
    echo "Starting iperf sender... link -> $TC_CONFIG"
 
    ./link.sh $TC_CONFIG &
    iperf3 -c $RECEIVER -t 120
    ./link.sh
fi

if [ "$ROLE" == "receiver-iperf" ]
then
    echo "Running iperf receiver."
    ls
    ip a
    iperf3 -s -1
fi
