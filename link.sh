#!/bin/bash

NIC=eth0
BURST=20kb
LATENCY=400

if [ $# -eq 0 ]
then
    tc qdisc delete dev $NIC root handle 1:
else
    tc qdisc add dev $NIC root handle 1: netem delay $5ms
    tc qdisc add dev $NIC parent 1: handle 2: tbf rate $1mbit burst $BURST latency ${LATENCY}ms

    sleep $3

    tc qdisc change dev $NIC root handle 1: netem delay $5ms
    tc qdisc change dev $NIC parent 1: handle 2: tbf rate $2mbit burst $BURST latency ${LATENCY}ms

    sleep $4

    tc qdisc change dev $NIC root handle 1: netem delay $5ms
    tc qdisc change dev $NIC parent 1: handle 2: tbf rate $1mbit burst $BURST latency ${LATENCY}ms

fi
